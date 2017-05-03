package main

import (
	"flag"

	"fmt"
	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/awserr"
	"github.com/aws/aws-sdk-go/aws/credentials"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/rds"
	"github.com/golang/glog"
	"os"
	"time"
)

type AwsShareSnapshot struct {
	SrcAccount    AwsAccount
	DestAccount   AwsAccount
	DBName        string
	RetentionTime float64
}

type AwsAccount struct {
	Region string

	AccessKeyID     string
	SecretAccessKey string
	AccountID       string
	Session         *session.Session
	RDSConnection   *rds.RDS
}

func (awsShareSnapshot *AwsShareSnapshot) dbSnapshotName() string {
	return fmt.Sprintf("%v-%v", awsShareSnapshot.DBName, time.Now().Format("2006-01-02"))

}

var (
	awsShareSnapshot AwsShareSnapshot
)

func init() {

	flagset := flag.NewFlagSet(os.Args[0], flag.ExitOnError)

	flagset.StringVar(&awsShareSnapshot.SrcAccount.Region, "src-region", os.Getenv("AWS_SRC_REGION"), "AWS source region")
	flagset.StringVar(&awsShareSnapshot.SrcAccount.AccessKeyID, "src-access-key-id", os.Getenv("AWS_SRC_ACCESS_KEY_ID"), "AWS source access key id")
	flagset.StringVar(&awsShareSnapshot.SrcAccount.SecretAccessKey, "src-secret-access-key", os.Getenv("AWS_SRC_SECRET_KEY"), "AWS source secret key")
	flagset.StringVar(&awsShareSnapshot.SrcAccount.AccountID, "src-account-id", os.Getenv("AWS_SRC_ACCOUNT_ID"), "AWS source account id")

	flagset.StringVar(&awsShareSnapshot.DestAccount.Region, "dest-region", os.Getenv("AWS_DEST_REGION"), "AWS destination region")
	flagset.StringVar(&awsShareSnapshot.DestAccount.AccessKeyID, "dest-access-key-id", os.Getenv("AWS_DEST_ACCESS_KEY_ID"), "AWS destination access key id")
	flagset.StringVar(&awsShareSnapshot.DestAccount.SecretAccessKey, "dest-secret-access-key", os.Getenv("AWS_DEST_SECRET_KEY"), "AWS destination access key")
	flagset.StringVar(&awsShareSnapshot.DestAccount.AccountID, "dest-account-id", os.Getenv("AWS_DEST_SECRET_KEY"), "AWS destination account id")

	flagset.Float64Var(&awsShareSnapshot.RetentionTime, "retention-time", 0, "Time in seconds to maintain the snapshot")
	flagset.StringVar(&awsShareSnapshot.DBName, "db-name", os.Getenv("DATABASE_NAME"), "Database name")

	flagset.Parse(os.Args[1:])
}

func main() {
	os.Exit(Main())
}

func Main() int {

	if err := awsShareSnapshot.HandleConnection(); err != nil {
		glog.Fatalf("%v", err)
	}

	err := awsShareSnapshot.TakeDBSnapshot()

	if err != nil {
		glog.Fatalf("Error taking snapshot: %v", err)
	}

	if err := awsShareSnapshot.WaitSnapshotFinish(awsShareSnapshot.SrcAccount.RDSConnection, awsShareSnapshot.dbSnapshotName()); err != nil {
		glog.Fatalf("Error waiting for snapshot: %v", err)
	}

	if err := awsShareSnapshot.ShareSnapshot(); err != nil {
		glog.Fatalf("Error sharing snapshot: %v", err)
	}

	if err := awsShareSnapshot.CopySnapshot(); err != nil {
		glog.Fatalf("Error copying snapshot: %v", err)
	}

	//delete snapsthot from origin
	if err := awsShareSnapshot.DeleteSnapshot(awsShareSnapshot.SrcAccount.RDSConnection, awsShareSnapshot.dbSnapshotName()); err != nil {
		glog.Fatalf("Error deleting snapshot: %v", err)
	}

	if err := awsShareSnapshot.SanitizeOldSnapshots(awsShareSnapshot.DBName); len(err) > 0 {
		glog.Fatalf("Error cleaning old snapshot: %v", err)
	}

	return 0

}

func (awsShareSnapshot *AwsShareSnapshot) HandleConnection() error {
	var err error

	awsShareSnapshot.SrcAccount.Session, err = session.NewSession(&aws.Config{
		Region:      &awsShareSnapshot.SrcAccount.Region,
		Credentials: credentials.NewStaticCredentials(awsShareSnapshot.SrcAccount.AccessKeyID, awsShareSnapshot.SrcAccount.SecretAccessKey, "")})

	if err != nil {
		return fmt.Errorf("Failed to connect to source account. Err: %v", err)
	}
	awsShareSnapshot.SrcAccount.RDSConnection = rds.New(awsShareSnapshot.SrcAccount.Session)

	awsShareSnapshot.DestAccount.Session, err = session.NewSession(&aws.Config{
		Region:      &awsShareSnapshot.DestAccount.Region,
		Credentials: credentials.NewStaticCredentials(awsShareSnapshot.DestAccount.AccessKeyID, awsShareSnapshot.DestAccount.SecretAccessKey, "")})

	if err != nil {
		return fmt.Errorf("Failed to connect to destination account. Err: %v", err)
	}

	awsShareSnapshot.DestAccount.RDSConnection = rds.New(awsShareSnapshot.DestAccount.Session)

	return err
}

func (awsShareSnapshot *AwsShareSnapshot) TakeDBSnapshot() error {

	_, err := awsShareSnapshot.SrcAccount.RDSConnection.CreateDBSnapshot(
		&rds.CreateDBSnapshotInput{
			DBInstanceIdentifier: &awsShareSnapshot.DBName,
			DBSnapshotIdentifier: aws.String(awsShareSnapshot.dbSnapshotName()),
		})

	if err != nil &&
		//we could accept error: already exist snapshot
		err.(awserr.Error).Code() != rds.ErrCodeDBSnapshotAlreadyExistsFault {

		return err

	}

	glog.Infof("Creating snapshot %v", awsShareSnapshot.dbSnapshotName())

	return nil
}

func (awsShareSnapshot *AwsShareSnapshot) ShareSnapshot() error {
	glog.Infof("Sharing snapshot %v between accounts %v and %v ", awsShareSnapshot.dbSnapshotName(), awsShareSnapshot.SrcAccount.AccountID,
		awsShareSnapshot.DestAccount.AccountID)

	_, error := awsShareSnapshot.SrcAccount.RDSConnection.ModifyDBSnapshotAttribute(
		&rds.ModifyDBSnapshotAttributeInput{DBSnapshotIdentifier: aws.String(awsShareSnapshot.dbSnapshotName()),
			AttributeName: aws.String("restore"), ValuesToAdd: []*string{aws.String(awsShareSnapshot.DestAccount.AccountID)}})

	return error

}

func (awsShareSnapshot *AwsShareSnapshot) WaitSnapshotFinish(conn *rds.RDS, dbSnapshotName string) error {

	//wait until snapshot finish
	for true {

		dbSnapshotsOutput, err := conn.DescribeDBSnapshots(
			&rds.DescribeDBSnapshotsInput{DBSnapshotIdentifier: aws.String(dbSnapshotName)})

		if err != nil {
			return err
		}

		for _, snapshot := range dbSnapshotsOutput.DBSnapshots {
			if *snapshot.Status == "available" {
				glog.Infof("Snapshot %v created ", dbSnapshotName)
				return nil
			}
		}
		glog.Infof("Wait until snapshot %v is not yet ready, waiting ...", dbSnapshotName)

		time.Sleep(time.Second * 10)

	}
	return nil
}

func (awsShareSnapshot *AwsShareSnapshot) CopySnapshot() error {
	dbCopyName := fmt.Sprintf("arn:aws:rds:%v:%v:snapshot:%v", awsShareSnapshot.SrcAccount.Region, awsShareSnapshot.SrcAccount.AccountID, awsShareSnapshot.dbSnapshotName())
	dbSnapname := "cp-" + awsShareSnapshot.dbSnapshotName()

	_, err := awsShareSnapshot.DestAccount.RDSConnection.CopyDBSnapshot(&rds.CopyDBSnapshotInput{SourceDBSnapshotIdentifier: aws.String(dbCopyName),
		TargetDBSnapshotIdentifier: aws.String(dbSnapname)})

	if err != nil &&
		//we could accept error: already exist snapshot
		err.(awserr.Error).Code() != rds.ErrCodeDBSnapshotAlreadyExistsFault {
		return err
	}

	return awsShareSnapshot.WaitSnapshotFinish(awsShareSnapshot.DestAccount.RDSConnection, dbSnapname)

}

func (awsShareSnapshot *AwsShareSnapshot) DeleteSnapshot(conn *rds.RDS, dbSnapshotname string) error {

	glog.Infof("Deleting snapshot %v ...", dbSnapshotname)

	_, err := conn.DeleteDBSnapshot(&rds.DeleteDBSnapshotInput{DBSnapshotIdentifier: aws.String(dbSnapshotname)})
	return err
}

func (awsShareSnapshot *AwsShareSnapshot) SanitizeOldSnapshots(dbName string) []error {

	var errors []error

	if awsShareSnapshot.RetentionTime > 0 {

		dbSnapshotsOutput, err := awsShareSnapshot.DestAccount.RDSConnection.DescribeDBSnapshots(
			&rds.DescribeDBSnapshotsInput{DBInstanceIdentifier: aws.String(awsShareSnapshot.DBName)})

		if err != nil {

			return append(errors, err)

		}

		for _, db := range dbSnapshotsOutput.DBSnapshots {
			maxRetentionTime := time.Now().Local().Add(-time.Second * time.Duration(awsShareSnapshot.RetentionTime))

			if *db.DBInstanceIdentifier == dbName && db.SnapshotCreateTime.Before(maxRetentionTime) {
				glog.Infof("Snapshot %v is too old, deleting..", *db.DBSnapshotIdentifier)

				if err := awsShareSnapshot.DeleteSnapshot(awsShareSnapshot.DestAccount.RDSConnection, *db.DBSnapshotIdentifier); err != nil {

					errors = append(errors, err)
				}
			}

		}
		return errors

	}
	glog.Infof("Sanitize is not enabled")

	return errors
}

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

	AccessKeyId     string
	SecretAccessKey string
	AccountId       string
	Session         *session.Session
	RDSConnection   *rds.RDS
}

func (snapshot *AwsShareSnapshot) dbSnapshotName() string {
	return fmt.Sprintf("%v-%v", awsShareSnapshot.DBName, time.Now().Day())

}

var (
	awsShareSnapshot AwsShareSnapshot
)

func init() {

	flagset := flag.NewFlagSet(os.Args[0], flag.ExitOnError)

	flagset.StringVar(&awsShareSnapshot.SrcAccount.Region, "src-region", os.Getenv("AWS_SRC_REGION"), "AWS source region")
	flagset.StringVar(&awsShareSnapshot.SrcAccount.AccessKeyId, "src-access-key-id", os.Getenv("AWS_SRC_ACCESS_KEY_ID"), "AWS source access key id")
	flagset.StringVar(&awsShareSnapshot.SrcAccount.SecretAccessKey, "src-secret-access-key", os.Getenv("AWS_SRC_SECRET_KEY"), "AWS source secret key")
	flagset.StringVar(&awsShareSnapshot.SrcAccount.AccountId, "src-account-id", os.Getenv("AWS_SRC_ACCOUNT_ID"), "AWS source account id")

	flagset.StringVar(&awsShareSnapshot.DestAccount.Region, "dest-region", os.Getenv("AWS_DEST_REGION"), "AWS destination region")
	flagset.StringVar(&awsShareSnapshot.DestAccount.AccessKeyId, "dest-access-key-id", os.Getenv("AWS_DEST_ACCESS_KEY_ID"), "AWS destination access key id")
	flagset.StringVar(&awsShareSnapshot.DestAccount.SecretAccessKey, "dest-secret-access-key", os.Getenv("AWS_DEST_SECRET_KEY"), "AWS destination access key")
	flagset.StringVar(&awsShareSnapshot.DestAccount.AccountId, "dest-account-id", os.Getenv("AWS_DEST_SECRET_KEY"), "AWS destination account id")

	flagset.Float64Var(&awsShareSnapshot.RetentionTime, "retention-time", 86400, "Time in seconds to maintain the snapshot")
	flagset.StringVar(&awsShareSnapshot.DBName, "db-name", os.Getenv("DATABASE_NAME"), "Database name")

	flagset.Parse(os.Args[1:])
}

func main() {
	os.Exit(Main())
}

func Main() int {

	if err := awsShareSnapshot.HandleConnection(); err != nil {
		glog.Fatalf("Error handle conenction %v", err)
	}

	err := awsShareSnapshot.TakeDBSnapshot()

	if err != nil {
		glog.Fatalf("Error taking snapshot: %v", err)
	}

	if err := awsShareSnapshot.WaitSnapshotFinish(); err != nil {
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

	if err := awsShareSnapshot.SanitizeOldSnapshots(); err != nil {
		glog.Fatalf("Error cleaning old snapshot: %v", err)
	}

	return 0

}

func (awsShareSnapshot *AwsShareSnapshot) HandleConnection() error {
	var err error

	awsShareSnapshot.SrcAccount.Session, err = session.NewSession(&aws.Config{
		Region:      &awsShareSnapshot.SrcAccount.Region,
		Credentials: credentials.NewStaticCredentials(awsShareSnapshot.SrcAccount.AccessKeyId, awsShareSnapshot.SrcAccount.SecretAccessKey, "")})

	if err != nil {
		return fmt.Errorf("Failed to connect to source account. Err: %v", err)
	}
	awsShareSnapshot.SrcAccount.RDSConnection = rds.New(awsShareSnapshot.SrcAccount.Session)

	awsShareSnapshot.DestAccount.Session, err = session.NewSession(&aws.Config{
		Region:      &awsShareSnapshot.DestAccount.Region,
		Credentials: credentials.NewStaticCredentials(awsShareSnapshot.DestAccount.AccessKeyId, awsShareSnapshot.DestAccount.SecretAccessKey, "")})

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

		return fmt.Errorf("Error running TakeDBSnapshot. Err: %v", err)

	} else {
		glog.Infof("Creating snapshot %v", awsShareSnapshot.dbSnapshotName())
	}

	return nil
}

func (awsShareSnapshot *AwsShareSnapshot) ShareSnapshot() error {
	_, error := awsShareSnapshot.SrcAccount.RDSConnection.ModifyDBSnapshotAttribute(
		&rds.ModifyDBSnapshotAttributeInput{DBSnapshotIdentifier: aws.String(awsShareSnapshot.dbSnapshotName()),
			AttributeName: aws.String("restore"), ValuesToAdd: []*string{aws.String(awsShareSnapshot.DestAccount.AccountId)}})

	return error

}

func (awsShareSnapshot *AwsShareSnapshot) WaitSnapshotFinish() error {

	//wait until snapshot finish
	for true {

		dbSnapshotsOutput, err := awsShareSnapshot.SrcAccount.RDSConnection.DescribeDBSnapshots(
			&rds.DescribeDBSnapshotsInput{DBSnapshotIdentifier: aws.String(awsShareSnapshot.dbSnapshotName())})

		if err != nil {
			return err
		}

		for _, snapshot := range dbSnapshotsOutput.DBSnapshots {
			if *snapshot.Status == "available" {
				glog.Infof("Snapshot %v created ", awsShareSnapshot.dbSnapshotName())
				return nil
			}
		}
		glog.Infof("Snapshot %v not yet ready, waiting..", awsShareSnapshot.dbSnapshotName())

		time.Sleep(time.Second * 10)

	}
	return nil //TODO FIX
}

func (awsShareSnapshot *AwsShareSnapshot) CopySnapshot() error {
	dbCopyName := fmt.Sprintf("arn:aws:rds:%v:%v:snapshot:%v", awsShareSnapshot.SrcAccount.Region, awsShareSnapshot.SrcAccount.AccountId, awsShareSnapshot.dbSnapshotName())

	_, err := awsShareSnapshot.DestAccount.RDSConnection.CopyDBSnapshot(&rds.CopyDBSnapshotInput{SourceDBSnapshotIdentifier: aws.String(dbCopyName),
		TargetDBSnapshotIdentifier: aws.String("cp-" + awsShareSnapshot.dbSnapshotName())})

	if err != nil &&
		//we could accept error: already exist snapshot
		err.(awserr.Error).Code() != rds.ErrCodeDBSnapshotAlreadyExistsFault {
		return fmt.Errorf("Error running CopySnapshot. Err: %v", err)

	}

	return nil

}

func (awsShareSnapshot *AwsShareSnapshot) DeleteSnapshot(conn *rds.RDS, dbSnapshotname string) error {
	_, err := conn.DeleteDBSnapshot(&rds.DeleteDBSnapshotInput{DBSnapshotIdentifier: aws.String(awsShareSnapshot.dbSnapshotName())})
	glog.Infof("Deleting snapshot ", awsShareSnapshot.dbSnapshotName())
	return err
}

func (awsShareSnapshot *AwsShareSnapshot) SanitizeOldSnapshots() error {

	dbSnapshotsOutput, err := awsShareSnapshot.DestAccount.RDSConnection.DescribeDBSnapshots(
		&rds.DescribeDBSnapshotsInput{DBInstanceIdentifier: aws.String(awsShareSnapshot.DBName)})

	if err != nil {
		return err
	}

	for _, db := range dbSnapshotsOutput.DBSnapshots {

		if time.Since(*db.SnapshotCreateTime).Seconds() > awsShareSnapshot.RetentionTime {
			glog.Infof("Snapshot % is too old, deleting..", db.DBSnapshotIdentifier)
			return awsShareSnapshot.DeleteSnapshot(awsShareSnapshot.DestAccount.RDSConnection, *db.DBSnapshotIdentifier)
		}

	}

	return nil
}

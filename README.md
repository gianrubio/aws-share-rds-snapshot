# aws-share-rds-snapshot

This project provides an easy way to share RDS snapshots between aws accounts. 

# TL;DR

Just run this command 
```
docker run gianrubio/aws-share-rds-snapshot  --src-secret-access-key=A... \
                                             --src-access-key-id=AKI... \
                                             --dest-account-id=213... \
                                             --dest-access-key-id=AKI.... \
                                             --dest-secret-access-key=B... \ 
                                             --dest-region=eu-west-1 \
                                             --src-region=eu-west-1 \
                                             --db-name=prod,preprod
                                             --src-account-id=752... 
                                             --retention-time=6014800
```

# How it works?

1. Snapshot an instance from the aws source account,
2. Share this snapshot to the aws destination account
3. Copy this backup in the destination account 
4. Delete generated snapshots in both account
5. Delete snapshots older than `retention-time` argument


# Required IAM policies
 
 
## Destination account
```
{
    "Version": "2012-10-17",
    "Statement": [
        {
            "Effect": "Allow",
            "Action": [
                "rds:CopyDBSnapshot",
                "rds:DeleteDBSnapshot",
                "rds:DescribeDBSnapshots"
            ],
            "Resource": [
                "*"
            ]
        }
    ]
}
```

## Source account 
```
{
    "Version": "2012-10-17",
    "Statement": [
        {
            "Effect": "Allow",
            "Action": [
                "rds:CreateDBSnapshot",
                "rds:DeleteDBSnapshot",
                "rds:ModifyDBSnapshotAttribute",
                "rds:DescribeDBSnapshots"
            ],
            "Resource": [
                "*"
            ]
        }
    ]
}
```

# Kubernetes

There's a cronjob example to run on k8s. Fill your secrets (base64) on `k8s/secrets.yaml`, edit `k8s/job.yaml` and apply the changes. 

`$ kubectl apply -f k8s/`
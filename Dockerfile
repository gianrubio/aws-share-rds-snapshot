FROM alpine:3.5


COPY build/aws-share-rds-snapshot /aws-share-rds-snapshot"
ENTRYPOINT ["/aws-share-rds-snapshot"]

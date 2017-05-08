FROM alpine:3.5

ENV USER=didi
RUN addgroup -g 1000 unprivileged && \
    adduser -G unprivileged -h /home/unprivileged -u 1000 -D unprivileged && \
    apk add ca-certificates --no-cache

USER unprivileged
WORKDIR /home/unprivileged

COPY build/aws-share-rds-snapshot /home/unprivileged/aws-share-rds-snapshot"
ENTRYPOINT ["/home/unprivileged/aws-share-rds-snapshot"]
FROM docker.io/library/golang:1.17 AS builder
WORKDIR /go/src/github.com/drogue-cloud/drogue-cloud-ocm-addon
COPY . .
ENV GO_PACKAGE github.com/drogue-cloud/drogue-cloud-ocm-addon

RUN make build --warn-undefined-variables

FROM registry.access.redhat.com/ubi8/ubi-minimal:latest

RUN microdnf update && microdnf clean all

COPY --from=builder /go/src/github.com/drogue-cloud/drogue-cloud-ocm-addon/drogue-cloud-ocm-addon /

CMD [ "/drogue-cloud-ocm-addon" ]
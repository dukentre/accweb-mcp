FROM golang:1.24-bookworm AS builder

ARG VERSION=docker
ARG COMMIT=unknown

ENV NODE_MAJOR=16

RUN apt-get update && apt-get install -y ca-certificates curl gnupg
RUN mkdir -p /etc/apt/keyrings
RUN curl -fsSL https://deb.nodesource.com/gpgkey/nodesource-repo.gpg.key | gpg --dearmor -o /etc/apt/keyrings/nodesource.gpg
RUN echo "deb [signed-by=/etc/apt/keyrings/nodesource.gpg] https://deb.nodesource.com/node_$NODE_MAJOR.x nodistro main" | tee /etc/apt/sources.list.d/nodesource.list

RUN apt-get update && export DEBIAN_FRONTEND=noninteractive \
	&& apt-get install -y git build-essential nodejs="${NODE_MAJOR}.*" zip

WORKDIR /accweb_src

COPY . /accweb_src

# RUN rm public/dist/*
RUN sh build/build_release.sh ${VERSION} ${COMMIT}
RUN cd /accweb_src/releases && unzip accweb_${VERSION}.zip && mv accweb_${VERSION} /accweb

FROM alpine:3.18

LABEL description="Assetto Corsa Competizione Server Management Tool via Web Interface."

ARG VERSION=noversion

RUN apk add --no-cache gettext wine ca-certificates

RUN mkdir -p /accserver /accweb/config /accweb/secrets /sslcerts

COPY --from=builder /accweb/accweb /accweb/accweb
COPY --from=builder /accweb_src/build/docker/* /accweb/

ENV ACCWEB_HOST=0.0.0.0:8080 \
	ACCWEB_TIMEOUT=20m \
	ACCWEB_ENABLE_TLS=false \
	ACCWEB_CERT_FILE=/sslcerts/certificate.crt \
	ACCWEB_PRIV_FILE=/sslcerts/private.key \
	ACCWEB_ADMIN_PASSWORD=weakadminpassword \
	ACCWEB_MOD_PASSWORD=weakmodpassword \
	ACCWEB_RO_PASSWORD=weakropassword \
	ACCWEB_ACC_SERVER_PATH=/accserver \
	ACCWEB_ACC_SERVER_EXE=accServer.exe \
	ACCWEB_LOGLEVEL=info \
	ACCWEB_CORS=* \
	ACCWEB_LOG_WITH_TIMESTAMP=true \
	ACCWEB_MCP_ENABLED=true \
	ACCWEB_MCP_TOKEN=change-me-mcp-token \
	ACCWEB_MCP_ALLOWED_ORIGINS= \
	ACCWEB_CALLBACK_ENABLED=false \
	ACCWEB_CALLBACK_TIMEOUT=500ms \
	ACCWEB_CALLBACK_URL= \
	ACCWEB_CALLBACK_HEADER_KEY= \
	ACCWEB_CALLBACK_HEADER_VALUE= \
	ACCWEB_CALLBACK_EVENTS=

VOLUME /accserver /accweb/config /accweb/secrets /sslcerts

WORKDIR /accweb

EXPOSE 8080
EXPOSE 8999/udp
EXPOSE 9231/udp
EXPOSE 9232/tcp

ENTRYPOINT [ "sh", "/accweb/docker-entrypoint.sh" ]

CMD [ "start" ]

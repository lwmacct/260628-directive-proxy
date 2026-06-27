# syntax=docker/dockerfile:1.7

FROM alpine:3.22

WORKDIR /app/data

RUN apk add --no-cache ca-certificates tini tzdata bash tar

ENV WEB_ROOT=/app/web
ENV APP_DATA=/app/data

COPY bin/app /usr/local/bin/app
COPY dist /app/web
COPY config/config.example.yaml /app/data/config/config.example.yaml

ENTRYPOINT ["tini", "--"]
CMD ["app"]

# Match the project's Playwright version while retaining its current Node/npm.
FROM node:26.12.0-trixie AS node
FROM mcr.microsoft.com/playwright:v1.64.1-resolute
COPY --from=node /usr/local/ /usr/local/
COPY certs/ /usr/local/share/ca-certificates/extra/
RUN update-ca-certificates >/dev/null && npm install --global npm@12.2.0
WORKDIR /repo/plugins/grafana
CMD ["npm", "run", "e2e", "--", "--project=chromium", "--workers=2"]

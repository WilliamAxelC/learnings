FROM ai-nimate-console:latest

WORKDIR /app

# Copy Go static daemon
COPY reefblast-bot ./
RUN chmod +x ./reefblast-bot

# Keep fallback Node files
COPY package.json ./
ENV PLAYWRIGHT_SKIP_BROWSER_DOWNLOAD=1
RUN npm install

COPY bot_runner.js ./
COPY reefblast_auto_progression.js ./
COPY credentials.txt ./

# Ensure logs directory exists
RUN mkdir -p /app/logs

ENV CHROME_BIN=/ms-playwright/chromium-1243/chrome-linux64/chrome \
    PLAYWRIGHT_BROWSERS_PATH=/ms-playwright

# Healthcheck to verify status.json is updated periodically
HEALTHCHECK --interval=30s --timeout=10s --retries=3 \
  CMD test $(find /app/logs/status.json -mmin -2 2>/dev/null) || exit 1

ENTRYPOINT []
CMD ["./reefblast-bot"]

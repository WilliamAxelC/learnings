FROM python:3.11-alpine

# Install ping and ca-certificates
RUN apk add --no-cache iputils-ping ca-certificates

WORKDIR /app

# Install python requests
RUN pip install --no-cache-dir requests

COPY ont_watchdog.py /app/ont_watchdog.py
RUN chmod +x /app/ont_watchdog.py

CMD ["python", "-u", "/app/ont_watchdog.py"]

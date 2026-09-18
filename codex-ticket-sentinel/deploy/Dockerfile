FROM python:3.12-slim

ENV PYTHONDONTWRITEBYTECODE=1 PYTHONUNBUFFERED=1
WORKDIR /app
RUN groupadd --gid 10001 sentinel \
    && useradd --uid 10001 --gid 10001 --no-create-home --shell /usr/sbin/nologin sentinel \
    && install -d -o 10001 -g 10001 -m 0700 /state
COPY --chown=10001:10001 sentinel.py /app/sentinel.py
USER 10001:10001
CMD ["python", "/app/sentinel.py", "--config", "/run/secrets/config.json"]

FROM python:3.11-slim

ENV PYTHONDONTWRITEBYTECODE=1 \
    PYTHONUNBUFFERED=1 \
    PORT=8091 \
    DOCLING_ARTIFACTS_PATH=/opt/docling-models \
    DOCLING_ARTIFACTS_MANIFEST=/opt/docling-models/SHA256SUMS \
    DOCLING_REQUIRE_LOCAL_ARTIFACTS=true

WORKDIR /app
COPY requirements.lock ./requirements.lock
RUN python -m pip install --no-cache-dir --requirement requirements.lock \
    && mkdir -p /opt/docling-models \
    && docling-tools models download --output-dir /opt/docling-models layout tableformer \
    && cd /opt/docling-models \
    && find . -type f ! -name SHA256SUMS -print0 | sort -z | xargs -0 sha256sum > SHA256SUMS \
    && pip freeze --local | sort > /opt/docling-models/python-dependencies.lock
COPY parser.py ./parser.py

USER nobody
EXPOSE 8091
CMD ["python", "parser.py"]

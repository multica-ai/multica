# syntax=docker/dockerfile:1.7

# --- Builder: install Python deps into a venv ---
# CUDA 12.1 runtime base — matches the torch+cu121 wheels we pin below. The
# devel image is ~3 GB heavier and we don't need nvcc at runtime. Python 3.12
# comes from deadsnakes because the 22.04 base ships 3.10 and plapre requires
# >= 3.12.
FROM nvidia/cuda:12.1.1-cudnn8-runtime-ubuntu22.04 AS builder

ENV DEBIAN_FRONTEND=noninteractive \
    PYTHONDONTWRITEBYTECODE=1 \
    PYTHONUNBUFFERED=1 \
    PIP_NO_CACHE_DIR=1 \
    PIP_DISABLE_PIP_VERSION_CHECK=1

RUN apt-get update && apt-get install -y --no-install-recommends \
      software-properties-common ca-certificates curl git \
    && add-apt-repository -y ppa:deadsnakes/ppa \
    && apt-get update && apt-get install -y --no-install-recommends \
      python3.12 python3.12-venv python3.12-dev \
      build-essential libsndfile1 \
    && rm -rf /var/lib/apt/lists/*

RUN python3.12 -m venv /opt/venv
ENV PATH="/opt/venv/bin:${PATH}"

WORKDIR /build
COPY pyproject.toml ./

# Install non-GPU deps first so the layer caches when only torch versions change.
RUN pip install --upgrade pip && pip install -e .

# GPU deps need the cu121 index. Pinning here (not in pyproject) keeps CPU-only
# `pip install -e .` working on dev boxes without dragging in the multi-GB
# torch wheel.
RUN pip install \
      --index-url https://download.pytorch.org/whl/cu121 \
      --extra-index-url https://pypi.org/simple \
      'torch>=2.5,<3.0' \
 && pip install \
      'transformers>=4.46,<5.0' \
      'safetensors>=0.4,<1.0' \
      'soundfile>=0.12,<1.0'

# Plapre lives on git, not PyPI. Pin a specific commit when we cut the first
# real-mode deploy so reproducible builds; for now `main` is fine because the
# container always boots in mock mode until HF access lands.
RUN pip install "plapre @ git+https://github.com/syv-ai/plapre.git@main"

# --- Runtime ---
FROM nvidia/cuda:12.1.1-cudnn8-runtime-ubuntu22.04 AS runtime

ENV DEBIAN_FRONTEND=noninteractive \
    PYTHONDONTWRITEBYTECODE=1 \
    PYTHONUNBUFFERED=1 \
    PATH="/opt/venv/bin:${PATH}" \
    HF_HOME=/models \
    TRANSFORMERS_CACHE=/models \
    HUGGINGFACE_HUB_CACHE=/models

RUN apt-get update && apt-get install -y --no-install-recommends \
      software-properties-common ca-certificates tini curl \
    && add-apt-repository -y ppa:deadsnakes/ppa \
    && apt-get update && apt-get install -y --no-install-recommends \
      python3.12 libsndfile1 \
    && rm -rf /var/lib/apt/lists/* \
    && groupadd --gid 1000 multica \
    && useradd --uid 1000 --gid 1000 --create-home --shell /bin/bash multica \
    && mkdir -p /models && chown -R multica:multica /models

COPY --from=builder /opt/venv /opt/venv

WORKDIR /app
COPY --chown=multica:multica app /app/app
COPY --chown=multica:multica scripts /app/scripts
RUN chmod +x /app/scripts/entrypoint.sh

USER multica

EXPOSE 8000

# tini reaps the python child cleanly so SIGTERM from Sliplane stops uvicorn
# without leaving zombie workers.
ENTRYPOINT ["/usr/bin/tini", "--", "/app/scripts/entrypoint.sh"]
CMD ["uvicorn", "app.main:app", "--host", "0.0.0.0", "--port", "8000"]

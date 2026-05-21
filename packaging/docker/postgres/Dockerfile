# packaging/docker/postgres/Dockerfile
#
# Multica's Postgres image: postgres:17 with pgvector built from source.
# Avoids the third-party pgvector/pgvector image while still using the
# official upstream postgres image as the base.

ARG PG_MAJOR=17
ARG PGVECTOR_VERSION=v0.7.4

# ── Build stage: compile pgvector against postgres-server-dev ─────────
FROM postgres:${PG_MAJOR} AS build

ARG PG_MAJOR
ARG PGVECTOR_VERSION

RUN apt-get update && apt-get install -y --no-install-recommends \
      build-essential \
      ca-certificates \
      git \
      postgresql-server-dev-${PG_MAJOR} \
    && rm -rf /var/lib/apt/lists/*

WORKDIR /src
RUN git clone --depth 1 --branch ${PGVECTOR_VERSION} \
      https://github.com/pgvector/pgvector.git
WORKDIR /src/pgvector
RUN make OPTFLAGS="" && make install

# ── Runtime stage: minimal additions to the official image ────────────
FROM postgres:${PG_MAJOR}

ARG PG_MAJOR

COPY --from=build /usr/lib/postgresql/${PG_MAJOR}/lib/vector.so \
                  /usr/lib/postgresql/${PG_MAJOR}/lib/
COPY --from=build /usr/share/postgresql/${PG_MAJOR}/extension/vector* \
                  /usr/share/postgresql/${PG_MAJOR}/extension/

# Healthcheck baked into the image so K8s probes don't need to install psql.
HEALTHCHECK --interval=10s --timeout=5s --start-period=30s --retries=5 \
  CMD pg_isready -U "$POSTGRES_USER" -d "$POSTGRES_DB" || exit 1

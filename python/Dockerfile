FROM python:3.13.12-slim-bookworm AS build
COPY --from=ghcr.io/astral-sh/uv:0.10.4 /uv /usr/local/bin/uv
WORKDIR /build
COPY python/pyproject.toml python/uv.lock ./
COPY python/workflow_sdk/ workflow_sdk/
RUN uv sync --frozen --no-dev --no-editable

FROM python:3.13.12-slim-bookworm
RUN groupadd --gid 10001 app && useradd --uid 10001 --gid app --create-home app
COPY --from=build /build/.venv /opt/venv
COPY plugins/ /plugins/
ENV PATH="/opt/venv/bin:$PATH" WORKFLOW_PLUGIN_ROOT=/plugins PYTHONDONTWRITEBYTECODE=1
LABEL maintainer="Brian Nunez" version="0.1.0" description="Workflow Python plugin execution service" platform="workflow"
USER app
EXPOSE 8090
ENTRYPOINT ["python", "-m", "workflow_sdk.server"]

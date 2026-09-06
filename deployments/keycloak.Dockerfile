FROM quay.io/keycloak/keycloak:26.7.3 AS build
ENV KC_DB=postgres
ENV KC_HEALTH_ENABLED=true
ENV KC_METRICS_ENABLED=true
ENV KC_HTTP_RELATIVE_PATH=/identity
RUN /opt/keycloak/bin/kc.sh build

FROM quay.io/keycloak/keycloak:26.7.3
COPY --from=build /opt/keycloak/ /opt/keycloak/
COPY deployments/keycloak/themes/forma /opt/keycloak/themes/forma
LABEL maintainer="Brian Nunez" version="26.7.3" description="App Builder identity provider" platform="workflow"
ENTRYPOINT ["/opt/keycloak/bin/kc.sh"]

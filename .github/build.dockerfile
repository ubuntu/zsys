FROM ubuntu:${DISTRO_VERSION}

# We install the certificats for GPROXY and protobuf/gettext for generator.
RUN apt update &&                     \
    apt install -y                    \
        ca-certificates               \
        protobuf-compiler gettext     \
        gcc libzfslinux-dev           \
        ${GOLANG_VERSION}

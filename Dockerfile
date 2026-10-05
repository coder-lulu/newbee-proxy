FROM alpine:3.21

# Define the project name | 定义项目名称
ARG PROJECT=proxy
# Define the config file name | 定义配置文件名
ARG CONFIG_FILE=proxy.yaml
# Define the author | 定义作者
ARG AUTHOR="example@example.com"

LABEL org.opencontainers.image.authors=${AUTHOR}

WORKDIR /app
ENV PROJECT=${PROJECT}
ENV CONFIG_FILE=${CONFIG_FILE}

# 仅拷贝编译后的二进制与配置（建议使用多阶段构建在 builder 阶段生成二进制 ./bin/proxy）
COPY ./bin/${PROJECT} ./
COPY ./etc/${CONFIG_FILE} ./etc/

EXPOSE 8889

ENTRYPOINT ["./proxy", "-f", "etc/proxy.yaml"]

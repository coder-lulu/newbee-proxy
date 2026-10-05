# Custom configuration | 独立配置
# Service name | 项目名称
SERVICE=NewbeeProxy
# Service name in specific style | 项目经过style格式化的名称
SERVICE_STYLE=proxy
# Service name in lowercase | 项目名称全小写格式
SERVICE_LOWER=proxy
# Service name in snake format | 项目名称下划线格式
SERVICE_SNAKE=proxy
# Service name in snake format | 项目名称短杠格式
SERVICE_DASH=proxy

# The main module path | 主模块路径
MAIN_MODULE_PATH=cmd/proxy.go

# The project version, if you don't use git, you should set it manually | 项目版本，如果不使用git请手动设置
VERSION=$(shell git describe --tags --always)

# The project file name style | 项目文件命名风格
PROJECT_STYLE=go_zero

# Whether to use i18n | 是否启用 i18n
PROJECT_I18N=true

# The suffix after build or compile | 构建后缀
PROJECT_BUILD_SUFFIX=api

# Swagger type, support yml,json | Swagger 文件类型，支持yml,json
SWAGGER_TYPE=json

# Ent enabled features | Ent 启用的官方特性
ENT_FEATURE=sql/execquery,intercept

# Auto generate API data for initialization | 自动生成 API 初始化数据
AUTO_API_INIT_DATA=true

# The arch of the build | 构建的架构
GOARCH=amd64

# The repository of docker | Docker 仓库地址
DOCKER_REPO=docker.io/xxx

# ---- You may not need to modify the codes below | 下面的代码大概率不需要更改 ----

GO ?= go
GOFMT ?= gofmt "-s"
GOFILES := $(shell find . -name "*.go")
LDFLAGS := -s -w

.PHONY: test
test: # Run test for the project | 运行项目测试
	go test -v --cover ./internal/..

.PHONY: fmt
fmt: # Format the codes | 格式化代码
	$(GOFMT) -w $(GOFILES)

.PHONY: lint
lint: # Run go linter | 运行代码错误分析
	golangci-lint run -D staticcheck

.PHONY: tools
tools: # Install the necessary tools | 安装必要的工具
	$(GO) install github.com/golangci/golangci-lint/cmd/golangci-lint@latest;
	$(GO) install github.com/go-swagger/go-swagger/cmd/swagger@latest

.PHONY: docker
docker: # Build the docker image | 构建 docker 镜像
	docker build -f Dockerfile -t $(DOCKER_REPO)/$(SERVICE_DASH)-$(PROJECT_BUILD_SUFFIX):$(VERSION) .
	@echo "Build docker successfully"

.PHONY: publish-docker
publish-docker: # Publish docker image | 发布 docker 镜像
	docker push $(DOCKER_REPO)/$(SERVICE_DASH)-$(PROJECT_BUILD_SUFFIX):$(VERSION)
	@echo "Publish docker successfully"

.PHONY: build-win
build-win: # Build project for Windows | 构建Windows下的可执行文件
	env CGO_ENABLED=0 GOOS=windows GOARCH=$(GOARCH) go build -ldflags "$(LDFLAGS)" -trimpath -o bin/$(SERVICE_STYLE).exe $(MAIN_MODULE_PATH)
	@echo "Build project for Windows successfully"

.PHONY: build-mac
build-mac: # Build project for MacOS | 构建MacOS下的可执行文件
	env CGO_ENABLED=0 GOOS=darwin GOARCH=$(GOARCH) go build -ldflags "$(LDFLAGS)" -trimpath -o bin/$(SERVICE_STYLE) $(MAIN_MODULE_PATH)
	@echo "Build project for MacOS successfully"

.PHONY: build-linux
build-linux: # Build project for Linux | 构建Linux下的可执行文件
	env CGO_ENABLED=0 GOOS=linux GOARCH=$(GOARCH) go build -ldflags "$(LDFLAGS)" -trimpath -o bin/$(SERVICE_STYLE) $(MAIN_MODULE_PATH)
	@echo "Build project for Linux successfully"

.PHONY: help
help: # Show help | 显示帮助
	@echo "可用命令:"
	@echo "  gen-proto    生成protobuf代码"
	@echo "  gen-api      生成API代码"
	@echo "  build        编译项目"
	@echo "  test         运行测试"
	@echo "  run          运行Agent服务"
	@echo "  clean        清理构建文件"
	@echo "  docker       构建Docker镜像"
	@echo "  help         显示帮助信息"

.PHONY: gen-proto
gen-proto:
	@echo "生成protobuf代码..."
	@cd proto && \
		protoc \
			--go_out=paths=source_relative:. \
			--go-grpc_out=paths=source_relative:. \
			agent.proto
	@echo "protobuf代码生成完成"

.PHONY: run
run:
	@echo "启动Newbee Proxy服务..."
	@go run cmd/proxy.go -f etc/proxy.yaml

.PHONY: clean
clean:
	@echo "清理构建文件..."
	@rm -rf bin/
	@rm -rf proto/*.pb.go
	@echo "清理完成"

.PHONY: dev
dev:
	@echo "启动热重载开发模式..."
	@air -c .air.toml

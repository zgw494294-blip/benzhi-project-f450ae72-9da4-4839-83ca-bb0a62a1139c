# BENZHI_README

## 项目说明
- 项目：benzhi-project-f450ae72-9da4-4839-83ca-bb0a62a1139c
- 项目用途：公共广播节目字幕交付质量审校服务，实现字幕编制、退修复审、批准签证和可验证事件时间线。
- Go 工具链：`golang:1.22`
- 前端工具链：无

## 标准构建、运行和测试命令
进入容器后执行：
cd '/app' && GOTOOLCHAIN=local go build ./...
cd /app && GOTOOLCHAIN=local go run ./cmd/captionqc -selfcheck -addr=127.0.0.1:19081
cd '/app' && GOTOOLCHAIN=local go test ./...

## Docker 构建和进入容器
chmod +x build_benzhi_docker.sh
./build_benzhi_docker.sh benzhi-project-f450ae72-9da4-4839-83ca-bb0a62a1139c-amd64 linux/amd64
./build_benzhi_docker.sh benzhi-project-f450ae72-9da4-4839-83ca-bb0a62a1139c-arm64 linux/arm64
docker run -it benzhi-project-f450ae72-9da4-4839-83ca-bb0a62a1139c-amd64:latest

## 题目验证命令
1. 预期退出码 0：`go test ./...`
2. 预期退出码 0：`go run ./cmd/captionqc -selfcheck -addr=127.0.0.1:19081`

#!/bin/sh

# 根据 proto go_package 或 文件名 生成对应的 golang model 定义

PROTO_DIR="./model/proto"
# 生成 Go 代码的根目录
OUT_DIR="./model"

# 查找所有 .proto 文件
find "$PROTO_DIR" -name "*.proto" | while read -r proto_file; do
  # 读取 proto 文件中的 go_package 选项
  go_package=$(grep "option go_package" "$proto_file" | sed -E 's/.*"([^"]+)".*/\1/')

  # 如果没有找到 go_package 选项，则使用文件名作为包名
  if [ -z "$go_package" ]; then
    # 获取文件名（不带扩展名）
    file_name=$(basename "$proto_file" .proto)
    # 使用文件名作为包名
    go_package="github.com/mcyouyou/unicore/model/$file_name"
  fi

  # 输出目录
  package_dir=$(echo "$go_package" | sed 's#github.com/mcyouyou/unicore/model/##')
  # 创建输出目录
  mkdir -p "$OUT_DIR/$package_dir"

  # 编译 proto 文件
  protoc -I="$PROTO_DIR" --go_opt=paths=source_relative --go_out="$OUT_DIR/$package_dir" "$proto_file"
done
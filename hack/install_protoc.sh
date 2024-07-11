#!/bin/bash

# 判断操作系统
OS="$(uname -s)"
case "${OS}" in
    Linux*)     os=linux;;
    Darwin*)    os=osx;;
    *)          os="UNKNOWN:${OS}"
esac

# 根据操作系统设置不同的链接
if [ "$os" == "linux" ]; then
    LINK="https://github.com/protocolbuffers/protobuf/releases/download/v27.2/protoc-27.2-linux-x86_64.zip"
elif [ "$os" == "osx" ]; then
    LINK="https://github.com/protocolbuffers/protobuf/releases/download/v27.2/protoc-27.2-osx-x86_64.zip"
else
    echo "Unsupported OS: ${OS}"
    exit 1
fi

echo "Downloading protoc for ${OS}"

# 进行下载和安装步骤
cd /usr/local && rm -f bin/protoc
rm -rf include/google/protobuf
rm -f readme.txt
wget $LINK -t 3 -O protoc.zip
unzip -o protoc.zip && rm -f protoc.zip

echo "protoc installed successfully!"

# 确保bin目录位于PATH中
if [ -n "$(echo $PATH | grep -o /usr/local/bin)" ]; then
    echo "Your PATH is correctly set."
else
    echo "Warning: Your PATH does not include /usr/local/bin."
    echo "You need to add /usr/local/bin to your PATH."
fi

go install google.golang.org/protobuf/cmd/protoc-gen-go@latest
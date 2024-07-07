# 定义变量
PROTO_DIR := ./model/proto
MODEL_OUT_DIR := ./model
GEN_MODEL_SCRIPT := ./hack/gen_proto_model.sh

# 默认目标
.PHONY: all
all: gen_model

# 编译 proto 文件
.PHONY: gen_model
gen_model:
	@$(GEN_MODEL_SCRIPT)

# 清理生成的文件
.PHONY: clean_model
clean_model:
	@echo "Cleaning up generated files..."
	@rm -rf $(MODEL_OUT_DIR)/*

# 重新编译
.PHONY: regen_model
regen_model: clean_model gen_model
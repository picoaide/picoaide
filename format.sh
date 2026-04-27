#!/bin/bash
# 将所有源代码文件中的制表符替换为两个空格
# 用法: ./format.sh [--check]
#   --check: 只检查不修改，有 tab 时退出码为 1

CHECK=false
if [ "$1" = "--check" ]; then
  CHECK=true
fi

FOUND=0

# 查找所有源代码文件（排除 vendor/node_modules/.git）
while IFS= read -r file; do
  if grep -q $'\t' "$file"; then
    if $CHECK; then
      echo "TAB: $file"
      FOUND=1
    else
      # 先用 expand 替换 tab 为空格，再写回
      # expand 默认 tab 宽度 8，我们需要 2 空格
      # 用 sed 更精确：只替换行首的 tab（缩进）
      sed -i 's/\t/  /g' "$file"
      echo "FIXED: $file"
    fi
  fi
done < <(find . \
  \( -name "*.go" -o -name "*.js" -o -name "*.html" -o -name "*.yaml" -o -name "*.yml" \
  -o -name "*.css" -o -name "*.json" -o -name "*.sh" -o -name "*.md" \) \
  ! -path "./vendor/*" \
  ! -path "./node_modules/*" \
  ! -path "./.git/*" \
  ! -path "./users/*" \
  ! -path "./archive/*" \
  2>/dev/null)

if $CHECK; then
  exit $FOUND
fi

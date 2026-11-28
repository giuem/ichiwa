# 官网插件页的示例索引

本地预览 `website/plugins.html` 用的示例插件索引，不会部署（只有 `website/` 会上传）。包括多个插件（够显示搜索框）、长名称、没有简介、没有图标（显示作者头像）、清单图标读不到、其他类型，以及几条应该被页面挡住的不合格或带 HTML 的条目。

```sh
dir=$(mktemp -d)
cp -R website/. "$dir"
cp -R testdata/website/. "$dir"     # 换成示例索引和图标
python3 -m http.server -d "$dir" 8000
# 打开 http://localhost:8000/plugins.html
```

不复制第二步就是仓库里的空索引；删掉 `$dir/plugins.json` 或写入坏的 JSON，可以看读取失败的提示。

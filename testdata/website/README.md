# 官网插件页的示例索引

本地预览 `website/plugins.html` 用的示例插件索引，不会部署（只有 `website/` 会上传）。包括：

- 多个插件（够显示搜索框）、长名称、长简介、没有简介、20 个 emoji 的名称；
- 有图标、没有图标（显示作者的 GitHub 头像）、图标文件不存在（退到头像）；
- 访问网站多于 3 个（显示“等 N 个网站”和“全部”）、带不合格主机；
- 不支持的 `kind` 和 `apiVersion`（没有安装按钮）、`minAppVersion`；
- 应该被页面挡住的条目：名称和简介里的 HTML、双向和零宽字符、不合格仓库、空名称、只差大小写的重复仓库、`blocked` 里的仓库、不是对象的条目。

图标照协议是重新编码过的 256×256 PNG，文件名是 `<id>.png`。

```sh
dir=$(mktemp -d)
cp -R website/. "$dir"
cp -R testdata/website/. "$dir"     # 换成示例索引和图标；不复制就是空索引
python3 -m http.server -d "$dir" 8000
# 打开 http://localhost:8000/plugins.html
```

删掉 `$dir/plugins.json` 或写入坏的 JSON 看读取失败的提示；写入 `{"version":2,"items":[]}` 看格式更新的提示。

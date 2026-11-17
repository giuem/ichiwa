package catalog

import "testing"

func TestEmojify(t *testing.T) {
	for in, want := range map[string]string{
		"哈哈[笑哭][笑哭]": "哈哈😂😂",
		"[吃瓜]看戏":     "🍉看戏",
		"[不认识]的代码":   "[不认识]的代码",
		"[[赞]":       "[👍",
		"没有括号":       "没有括号",
		"只有一半[笑":     "只有一半[笑",
		"a]b[爽]c":    "a]b😎c",
	} {
		if got := emojify(in); got != want {
			t.Errorf("emojify(%q) = %q, want %q", in, got, want)
		}
	}
}

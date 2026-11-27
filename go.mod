module github.com/giuem/ichiwa

go 1.27.3

require (
	github.com/beilunyang/openhongguo v0.4.1
	github.com/ebitengine/purego v0.11.3
	github.com/egoist/mygo v0.10.0
	github.com/gen2brain/heic v0.7.2
	golang.org/x/image v0.48.0
)

require (
	github.com/MoeForge/moego v0.1.0-alpha.3 // indirect
	github.com/MoeForge/moejs v0.1.1 // indirect
	github.com/ProtonMail/go-crypto v1.4.1 // indirect
	github.com/cloudflare/circl v1.6.2 // indirect
	github.com/go-text/typesetting v0.3.5 // indirect
	github.com/klauspost/compress v1.18.6 // indirect
	github.com/tetratelabs/wazero v1.12.0 // indirect
	github.com/ulikunitz/xz v0.5.15 // indirect
	go.digitalxero.dev/rpm v0.3.4 // indirect
	golang.org/x/crypto v0.57.0 // indirect
	golang.org/x/net v0.59.0 // indirect
	golang.org/x/sys v0.50.0 // indirect
	golang.org/x/text v0.44.0 // indirect
)

tool github.com/egoist/mygo/cmd/mygo

// openhongguo v0.4.1 拿不到播放地址（服务端 2026-11-23 起要求播放请求带 x-medusa）。
// giuem/openhongguo 是 v0.4.1 加上游 PR #25 的修复；上游发布新版后删掉这行。
replace github.com/beilunyang/openhongguo => github.com/giuem/openhongguo v0.4.2-0.20261126170640-ad6a1944dd89

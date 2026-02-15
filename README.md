# ytv1

Go-native YouTube extractor rewrite.

## Repo layout

- `legacy/kkdai-youtube`: original kkdai/youtube snapshot for reference
- `internal/*`: new extractor core
- `cmd/ytv1`: executable entrypoint

## Design goals

- No Python runtime dependency
- Fast adaptation to YouTube protocol changes
- Clear separation: client policy, player JS resolution, challenge solving, stream assembly

## Package Usage

`ytv1` is a library-first project. Use the `client` package directly:

```go
package main

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/mjmst/ytv1/client"
)

func main() {
	c := client.New(client.Config{})

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	info, err := c.GetVideo(ctx, "jNQXAC9IVRw")
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println(info.Title)
}
```

### Config

`client.Config` 주요 필드:

- `HTTPClient`: 사용자 정의 HTTP 클라이언트
- `ProxyURL`: 프록시 URL
- `PoTokenProvider`: PO token 공급자
- `ClientOverrides`: Innertube 클라이언트 시도 순서 강제
- `PlayerJSBaseURL` / `PlayerJSUserAgent` / `PlayerJSHeaders`: Player JS fetch 동작 제어

### Public API

- `GetVideo(ctx, input)`: 메타데이터 + 포맷 목록
- `GetFormats(ctx, input)`: 포맷 목록만 반환
- `ResolveStreamURL(ctx, videoID, itag)`: cipher 포맷의 최종 재생 URL 해석
- `FetchDASHManifest(ctx, input)`: DASH manifest 원문 fetch
- `FetchHLSManifest(ctx, input)`: HLS manifest 원문 fetch
- `Download(ctx, input, options)`: 선택한 스트림을 파일로 다운로드

### Error Handling

패키지 레벨 에러:

- `client.ErrInvalidInput`
- `client.ErrUnavailable`
- `client.ErrLoginRequired`
- `client.ErrNoPlayableFormats`
- `client.ErrChallengeNotSolved`
- `client.ErrAllClientsFailed`

예시:

```go
if err != nil {
	switch err {
	case client.ErrLoginRequired:
		// 인증 필요
	case client.ErrUnavailable:
		// 영상 비공개/차단/삭제 등
	default:
		// 기타
	}
}
```

## CLI (Smoke Test)

CLI는 패키지 검증용 얇은 어댑터입니다.

- 메타데이터 조회: `ytv1.exe -v <video_id>`
- player base.js URL 확인: `ytv1.exe -v <video_id> -playerjs`
- 다운로드: `ytv1.exe -v <video_id> -download [-itag <itag>] [-o <output_path>]`
- 우회 실험용: `-clients <a,b,c>`, `-visitor-data <VISITOR_INFO1_LIVE>`

# 2026-09-05 감사 후속 수정

[원본 감사](AUDIT_2026-09-05.md)의 F01–F43 및 O01–O08을 수정했다. 원본 보고서의 위치와 재현 설명은 수정 전 코드 기준이며, 아래 표가 후속 처리 기록이다. 공개 최소 API는 유지하고 필요한 필드·설정·메서드만 추가했다.

## 결함별 처리

| 항목 | 수정 결과 | 구현·검증 근거 |
|---|---|---|
| F01 | 실패한 병렬 청크 파일을 연속 완료된 prefix까지 잘라 안전하게 재개한다. 416은 원격 전체 길이가 로컬 길이와 같을 때만 완료로 인정한다. | [다운로더](../client/download.go), `TestChunkFailureResumePreservesOnlyCompletePrefix` |
| F02 | puremux 실패가 이전 정상 출력을 삭제하지 않는다. 임시 출력의 성공 시 교체를 유지한다. | [muxer 회귀 테스트](../internal/muxer/audit_safety_test.go) |
| F03 | 아카이브를 O_APPEND로 열어 독립 핸들의 기록이 덮어써지지 않는다. | `TestArchiveIndependentHandlesAppend` |
| F04 | AES IV를 16바이트로 정규화하고 잘못된 hex·길이를 오류로 반환한다. | [파서 안전성 테스트](../internal/downloader/audit_safety_test.go) |
| F05 | DASH duration의 끝 인덱스·단위·산술 오버플로를 검사한다. | 동일 테스트 파일 |
| F06 | WebSocket handshake 쓰기 deadline을 해제하고 프레임마다 설정한다. 쓰기는 직렬화하고 heartbeat 실패를 읽기 루프에 전달한다. | [WebSocket 테스트](../internal/source/soop/audit_safety_test.go), [agentstream](../internal/source/soop/agentstream.go) |
| F07 | source 스트림의 HTTP 요청도 반환 reader의 취소 context를 사용한다. | `TestSourceStreamCloseCancelsPendingRequest` |
| F08 | 반환·캐시 메타데이터의 Parts와 source 헤더를 복제한다. | `TestClonedPartsAreIndependent`, race 검사 |
| F09 | muxer의 입력 삭제를 제거하고 호출자가 KeepIntermediateFiles 정책에 따라 정리한다. | [puremux 테스트](../internal/muxer/puremux_test.go), [다운로드 테스트](../client/download_test.go) |
| F10 | OpenFormatStream이 HLS/DASH 매니페스트를 미디어 바이트로 전개한다. | [공통 미디어 경로](../client/media_stream.go), [스트림 API](../client/stream_api.go) |
| F11 | source의 직접 HTTP MPEG-TS와 HLS를 포맷 프로토콜·URL로 구별한다. | `TestSourceDirectStreamSelectionAndCoalescing` |
| F12 | source도 공통 Itag/Mode 선택자를 사용한다. | 같은 테스트 및 [source 테스트](../client/source_dispatch_test.go) |
| F13 | 파일 병합용 영상·음성 다운로드를 함께 시작하고 오류·취소를 전파한 뒤 둘 다 종료를 기다린다. | [병합 구현](../client/download.go), lifecycle 테스트 |
| F14 | MP3 transcoder에 해석된 미디어 바이트와 source 헤더를 적용한다. | `TestMP3HLSUsesMediaAndPartFile` |
| F15 | MP3 출력도 stdout과 .part → 최종 파일 경로를 사용한다. | [출력 경로](../client/media_stream.go), 같은 테스트 |
| F16 | DASH 직접 BaseURL 파일과 segmented MPD를 구별하고 template-only representation을 노출한다. | [포맷 파서](../internal/formats/dash.go), manifest integration 테스트 |
| F17 | DASH 초기화 데이터를 해당 Period의 첫 미디어 앞에 기록한다. | `TestDASHInheritedNumberTemplatePeriodsAndInit` |
| F18 | r=-1을 다음 S의 t 또는 presentation 경계로 확장하고 마지막 부분 세그먼트를 포함한다. 경계를 알 수 없는 dynamic MPD는 명확한 오류다. | [DASH 주소 전개](../internal/downloader/dash_addressing.go), duration 테스트 |
| F19 | duration/Number, 형식 지정 Number/Time/Bandwidth, 상위 template/BaseURL 상속, 여러 Period를 처리한다. 선택 representation이 없는 Period는 누락시키지 않고 오류로 반환한다. | 같은 구현·테스트 |
| F20 | EXTINF 뒤의 태그를 URI로 오인하지 않는다. 명시·암시 byte range, Range/Content-Range 검증, 같은 URI의 다른 범위를 처리한다. | `TestHLSByteRangeAfterExtinf` |
| F21 | 지원하지 않는 암호화/KEYFORMAT은 오류다. AES-128 초기화 데이터도 키·IV에 맞춰 복호화한다. | [암호화 회귀 테스트](../internal/downloader/audit_encryption_test.go) |
| F22 | 빈 미디어·초기화 조각을 성공으로 기록하지 않는다. | 동일 파일 및 DASH empty-segment 테스트 |
| F23 | PTS wrap/reset·초기 PTS 누락을 처리하고 미완성 세그먼트에 별도 크기 상한을 둔다. | [DVR 회귀 테스트](../internal/timeshift/audit_test.go) |
| F24 | PMT codec을 보존하고 여러 TS 패킷에 걸친 H.264 IDR/HEVC IRAP를 판별한다. | `TestHEVCKeyframeAcrossPackets` |
| F25 | Finish는 ENDLIST와 마지막 파일을 유지하고 Close가 정리한다. 파일을 보유 잠금 아래에서 열어 eviction과 요청 사이의 경합을 막는다. | `TestDiskPlaybackAfterFinish`, DVR tests/race |
| F26 | SOOP endpoint 캐시를 Source 인스턴스별로 격리하고 종료·idle 만료·Close와 연결한다. detached 캐시에도 유효기간을 둔다. | [agentwire](../internal/source/soop/agentwire.go), [agentstream 테스트](../internal/source/soop/agentstream_test.go) |
| F27 | 전체 스트림을 감싸던 sync.Once를 원자적 1회 claim으로 바꿔 중복 GET에 즉시 409를 반환한다. | [agentHandler](../internal/source/soop/agentserve.go) |
| F28 | 기존 text 자막과 srv3의 p/s 자막, 밀리초 시간, 인라인 텍스트를 처리한다. | `TestSrv3CaptionTextAndTiming` |
| F29 | ext!= 연산자를 보존하고 부등 비교한다. | [selector 회귀 테스트](../internal/selector/audit_test.go) |
| F30 | 빈 그룹·잘못된 괄호·후행 문자열·빈 값·잘못된 숫자를 거절한다. | 동일 파일 |
| F31 | 범위 밖 역순 선택은 빈 결과를 반환하고 step 덧셈 오버플로를 막는다. | `TestEmptyReverseRangeAndStepOverflow` |
| F32 | cipher·manifest·source·Parts 주소 만료를 함께 계산하고 재추출 후 새 itag URL을 사용한다. 이미 해석한 MPD URL을 옛 주소로 되돌리지 않는다. | [캐시 회귀 테스트](../client/audit_cache_test.go), [client](../client/client.go) |
| F33 | PO 토큰에 설정 가능한 TTL과 캐시 우회 옵션을 추가한다. | `TestTokenExpiryCallsProviderAgain` |
| F34 | raw.SourceClient를 우선해 포맷별 POT 정책을 적용한다. | [resolver](../client/client.go), POT URL 테스트 |
| F35 | 공개 ResolveStreamURL도 materialized manifest 포맷을 조회한다. | 같은 resolver와 manifest tests |
| F36 | 썸네일에 timeout·공통 헤더·초과 크기 검사를 적용하고 임시 파일 성공 후 교체한다. | `TestThumbnailFailurePreservesExistingFile` |
| F37 | 템플릿을 한 번만 치환하여 삽입된 제목 안의 토큰이 다시 해석되지 않는다. | `TestTemplateDoesNotExpandInsertedValues` |
| F38 | dynamic DASH에서 현재 MPD의 안정적인 Period/URL identity를 유지한다. 4096개에서 기록을 통째로 지우지 않는다. | [DASH 다운로드 루프](../internal/downloader/dash.go), dynamic timeline tests |
| F39 | 파일명 렌더링에 쓰는 메타데이터와 muxer에 전달하는 메타데이터를 분리한다. | [downloadAndMerge](../client/download.go), NoEmbedMetadata tests |
| F40 | SourceName/WebpageURL을 전달하고 JSON extractor·웹 주소와 source-qualified 아카이브를 사용한다. | `TestSourceIdentityJSONAndArchive` |
| F41 | Range probe의 응답 본문을 전부 읽지 않고 상태·헤더로 판단해 닫는다. | [probeContentLengthWithRange](../client/download.go), 다운로드 tests |
| F42 | -g의 n/POT 포맷도 resolver를 거친다. JSON은 선택 옵션을 적용하고 해석된 URL·requested_formats·요청 헤더를 내보낸다. | [PlaybackJSON](../client/playback_json.go), CLI·URL tests |
| F43 | manifest 요청에 공통 헤더를 적용하고 파싱 오류를 전달한다. | [HLS](../internal/formats/hls.go), [DASH](../internal/formats/dash.go), manifest tests |

## 성능·동시성 개선

| 항목 | 결과 |
|---|---|
| O01 | GetVideo/GetFormats의 유효 캐시 fast path와 잠금 후 재확인. source도 입력별로 추출을 합친다. 32개 동시 source 요청에서 실제 Extract 1회를 검증했다. |
| O02 | 저장 시 URL 만료를 계산하고 조회는 시간 비교만 수행한다. 기본 session 상한은 256개이며 LRU로 퇴출한다. 오래된 항목의 전체 sweep은 최대 분당 한 번이다. |
| O03 | HLS는 전체 playlist 수만큼 goroutine·채널을 만들지 않고 고정 작업자와 순환 슬롯을 사용한다. |
| O04 | DASH도 같은 ordered writer를 사용해 앞 조각부터 기록·해제한다. MaxBufferedBytes/MaxSegmentBytes로 동시 작업 수를 제한한다. |
| O05 | client/playerjs/Innertube/token 잠금 대기가 context 취소를 따른다. callback은 동기 호출이며 동시 호출 안전성과 재진입 제한을 설정 문서에 명시했다. |
| O06 | DVR handler를 한 번 만들고 첫 세그먼트 대기는 ready/종료 신호와 context를 사용한다. |
| O07 | HLS 트랙을 동시에 가져오고 각 트랙이 독립적으로 다음 조각을 연다. 번호·개수·길이가 다른 트랙을 패킷 타임스탬프로 섞으며, packed AAC 타임스탬프가 매 조각 초기화되지 않도록 이어간다. |
| O08 | Muxer 설명을 실제 오류 계약과 맞추고 아래 지원·검증 범위 및 전후 벤치마크를 기록했다. |

### 같은 fixture의 전후 벤치마크

기준 커밋 `9ca90c0`과 수정 작업 트리에 동일한 benchmark 파일을 적용했다. Windows/amd64, i9-9900K, `go test -run '^$' -bench BenchmarkAudit -benchmem -benchtime=1s -count=3`의 중앙값이다. 실제 CDN 처리량 측정이 아니다.

| 벤치마크 | 수정 전 | 수정 후 |
|---|---:|---:|
| 200개 URL이 있는 캐시 세션 조회 | 102,326 ns/op; 115,200 B/op; 1,000 allocs/op | 42.34 ns/op; 0 B/op; 0 allocs/op |
| HLS 1,000 조각 × 256 bytes, 동시 작업 4개 | 2,598,529 ns/op; 2,505,993 B/op | 2,146,839 ns/op; 1,999,992 B/op |

HLS fixture는 약 17% 빨라졌고 할당 바이트는 약 20% 줄었다. 캐시 결과는 URL 파싱 제거 효과만 측정한다. 초기 추출이나 실제 네트워크 전체가 같은 비율로 빨라진다는 의미는 아니다.

벤치마크 코드는 [client](../client/audit_benchmark_test.go), [downloader](../internal/downloader/audit_benchmark_test.go)에 있다. 원시 로그는 `F:\cache\ytv1-audit-20260905\benchmark-{before,after}.log`, 기준 작업 트리는 `F:\cache\ytv1-audit-baseline`에 보관했다.

## API·설정과 지원 범위

- 추가 API: `Client.Close`, `Client.ResolveVideoURLs`, `Client.PlaybackJSON`, `ArchiveID`.
- 추가 데이터: `VideoInfo.SourceName/WebpageURL`, `FormatInfo.ManifestURL/RepresentationID`, JSON `requested_formats/http_headers`.
- `SessionCacheMaxEntries`: 0이면 256, 음수이면 개수 제한 해제. `SessionCacheTTL`: 기존처럼 0이면 6시간, 음수이면 로컬 TTL 해제. URL 자체 만료는 계속 검사한다.
- `PoTokenCacheTTL`: 0이면 5분, 음수이면 provider에 매번 위임한다. provider가 더 짧은 토큰 수명을 쓰면 이에 맞게 설정한다.
- segmented transport의 `MaxBufferedBytes`: 기본 512 MiB의 동시 payload 예산. `MaxSegmentBytes`: 기본 100 MiB. 작업 수는 예산/조각 상한으로 제한한다. 실제 RSS에는 읽기 버퍼·파싱·muxer 메모리가 추가된다.
- DVR의 `MaxSegmentBytes`: 기본 64 MiB. MaxBytes가 더 작으면 그 값을 사용한다. 정상 keyframe 없이 이를 넘으면 무한 축적 대신 오류를 반환한다. `Finish` 후 재생을 제공하다가 서버 종료 시 `Close`로 정리한다.

| 입력/출력 | 파일 | stdout | OpenStream | MP3 Download |
|---|---|---|---|---|
| YouTube/source 직접 HTTP 미디어 | 지원; Range 지원 시 재개 | 지원 | 미디어 reader | 설정한 transcoder 사용 |
| HLS 단일 트랙·muxed 트랙·Parts | 조각 전개; part 파일 | 조각 전개 | 미디어 reader | 조각 전개 후 변환 |
| DASH 직접 BaseURL | 직접 HTTP 파일로 처리 | 지원 | 미디어 reader | 직접 미디어 변환 |
| DASH SegmentTemplate | 초기화·timeline/Number 전개 | 지원 | 미디어 reader | 전개 후 변환 |
| 분리된 HLS 영상+음성 | 설정한 파일 muxer | 네이티브 MPEG-TS mux | 네이티브 MPEG-TS mux | 선택한 음성 트랙 변환 |
| 그 외 분리된 영상+음성 | 설정한 파일 muxer | 기존 지원 제한 유지 | 기존 단일 포맷 선택 계약 | 선택한 음성 트랙 변환 |

MP3 인코딩은 사용자가 설정한 transcoder를 요구한다. HLS의 SAMPLE-AES/비 identity 키와 경계가 없는 dynamic DASH 음수 반복은 지원을 가장해 성공하지 않고 오류를 반환한다. DASH 여러 Period에서 선택 representation이 없는 경우도 오류다. 파일 archive는 완료 기록이며 다른 프로세스의 다운로드 작업을 예약하는 분산 잠금은 아니다.

## 검증

- `go test ./...`, `go vet ./...`, `go test -race ./...`, `git diff --check`: 모두 통과. 원시 결과는 `F:\cache\ytv1-audit-20260905\fixes-*.log`에 보관했다.
- 파일 손상·기존 출력 보존·자막·선택자·암호화·DASH 초기화/Period·DVR 종료/PTS·source 취소·Parts 소유권·동시 추출·packed AAC 연속성을 위한 회귀 테스트를 추가했다.
- 병렬 트랙 다운로드가 노출한 테스트 callback의 공유 slice 경합도 mutex로 수정했다. 실제 callback은 문서화한 동시 호출 계약을 따른다.
- YouTube/SOOP 실서버 장시간 방송과 외부 MP3/FFmpeg 바이너리 실행은 이번 로컬 fixture 검증에 포함하지 않았다.


## v0.2.9 릴리스 의존성 검증

puremux를 v0.2.2로 갱신한 상태에서 전체 테스트·vet·race 검사와 non-CGO CLI 빌드를 다시 통과했다. 이 버전의 엄격한 메타데이터 검사에 맞춰 네이티브 파일/라이브 mux 경로에 AllowMetadataLoss를 명시했다. 출력 컨테이너가 지원하지 않는 트랙 이름·disposition은 생략할 수 있지만 코덱·타이밍 오류는 계속 실패하며, 요청한 메타데이터 임베딩은 기존 fallback 정책을 따른다. 위 전후 벤치마크 수치는 의존성 갱신 전에 측정한 값이다.

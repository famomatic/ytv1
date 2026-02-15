# PlayerJS Fixture Rotation

`internal/playerjs/testdata/basejs_fixture.js` is a real captured YouTube `base.js` fixture.
Rotate it when parser regression is suspected.

## Capture Flow

1. Build CLI:
   - `go build -o ytv1.exe ./cmd/ytv1`
2. Print current player JS URL:
   - `./ytv1.exe -v jNQXAC9IVRw -playerjs`
3. Run update script with returned URL:
   - `powershell -ExecutionPolicy Bypass -File scripts/update_playerjs_fixture.ps1 -PlayerJSURL "<url>"`
4. Run regression tests:
   - `go test ./internal/playerjs -run Fixture -v`
   - `go test ./...`

## Notes

- Keep synthetic fixtures for stable parser-shape tests:
  - `synthetic_basejs_fixture.js`
  - `synthetic_basejs_fixture_v2.js`
  - `synthetic_basejs_fixture_v3.js`
- Real fixture updates should not remove synthetic fixtures.
- If real fixture fails while synthetic passes, parser pattern expansion is required.

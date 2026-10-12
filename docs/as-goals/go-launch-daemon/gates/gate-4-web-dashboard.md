# Gate: web-dashboard

## Condition
The operator console — built with Vite + React + Ant Design at latest npm versions — is fully usable through the sandbox's single origin: service list with live status, start/stop/restart actions with confirmation and feedback, per-service log viewer with live streaming, and daemon health info. Status changes propagate without manual refresh (WebSocket), and the layout is responsive. The user-visible entry is the Next.js portal at `/` (port 3000) embedding the daemon-served console.

## Evidence Required
- [ ] Artifact 1: frontend project (Vite + React + antd latest, TypeScript) → `launch-daemon/frontend/package.json`, `launch-daemon/frontend/vite.config.ts`, `launch-daemon/frontend/src/`
- [ ] Artifact 2: gateway-aware API client — every request relative with `XTransformPort=8800` query param; WS `wss://{location.host}/api/v1/events?XTransformPort=8800` with reconnect + polling fallback → `launch-daemon/frontend/src/api/client.ts`
- [ ] Artifact 3: console pages — Services table (status Badge, PID, uptime, restarts, last exit, actions), Service detail (metadata, policy, log viewer with autoscroll/pause) → `launch-daemon/frontend/src/pages/`
- [ ] Artifact 4: Next.js portal page embedding the console (full-viewport iframe → `/?XTransformPort=8800`, slim header, sticky-footer-safe layout) → `src/app/page.tsx`
- [ ] Artifact 5: agent-browser verification transcript + screenshots (desktop ≥1280px and mobile 375px) demonstrating: portal loads console → table renders live services → stop a service via UI → status changes WITHOUT manual refresh → logs stream live → saved under `docs/as-goals/go-launch-daemon/[iteration]/evidence/`
- [ ] Artifact 6: daemon serves the built console (`-static` → `frontend/dist`, no-cache on `index.html`) → `launch-daemon/daemon/internal/api/static.go`

## Verification Method
Test Engineer (+ Frontend Engineer providing browser evidence): run daemon + console build; open `http://localhost:3000/` with agent-browser; execute the golden path above; cross-check dev.log for API/WS errors. Any absolute-URL request, CORS error, manual-refresh-required status change, or broken control action = Fail. UX standards checked: loading states, error feedback (antd message), confirmations on destructive actions, long log lists capped + scrollable.

## Owner
Frontend Engineer (console + browser evidence); Senior Software Engineer (daemon static serving + portal page); System Test Engineer (verdict)

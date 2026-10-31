import { ApiError, authRequest, endpoints, type Session } from "../api";
import type { ConnectionStatus, ServerEvent } from "./types";

export class CommandError extends Error {
  constructor(public code: string, message: string) { super(message); }
}

// getRandomValues also works on HTTP LAN origins, where randomUUID is unavailable.
export function newID() {
  const bytes = crypto.getRandomValues(new Uint8Array(16));
  bytes[6] = (bytes[6] & 15) | 64;
  bytes[8] = (bytes[8] & 63) | 128;
  const hex = Array.from(bytes, b => b.toString(16).padStart(2, "0")).join("");
  return `${hex.slice(0, 8)}-${hex.slice(8, 12)}-${hex.slice(12, 16)}-${hex.slice(16, 20)}-${hex.slice(20)}`;
}

export class ChatSocket {
  private socket?: WebSocket;
  private stopped = true;
  private attempts = 0;
  private retry?: ReturnType<typeof setTimeout>;
  private handshake?: ReturnType<typeof setTimeout>;
  private pending = new Map<string, { resolve: (data: unknown) => void; reject: (e: Error) => void; timer: ReturnType<typeof setTimeout> }>();
  private ready = false;

  constructor(private userID: string, private status: (status: ConnectionStatus) => void,
    private event: (event: ServerEvent) => void, private failure: (message: string) => void) {}

  start() { this.stopped = false; this.connect(); }
  stop() {
    this.stopped = true;
    clearTimeout(this.retry); clearTimeout(this.handshake);
    this.ready = false;
    const ws = this.socket;
    this.socket = undefined;
    ws?.close();
    this.rejectPending();
  }
  reconnect() { this.stop(); this.attempts = 0; this.start(); }

  request<T>(type: string, payload: object = {}): Promise<T> {
    if (!this.ready || this.socket?.readyState !== WebSocket.OPEN) return Promise.reject(new Error("Connection unavailable. Reconnect to try again."));
    const requestId = newID();
    return new Promise<T>((resolve, reject) => {
      const timer = setTimeout(() => {
        this.pending.delete(requestId);
        reject(new Error("No confirmation received. The request may have reached the server."));
      }, 10000);
      this.pending.set(requestId, { resolve: data => resolve(data as T), reject, timer });
      try { this.socket!.send(JSON.stringify({ v: 1, type, requestId, payload })); }
      catch { clearTimeout(timer); this.pending.delete(requestId); reject(new Error("Connection lost before confirmation.")); }
    });
  }

  private rejectPending() {
    for (const item of this.pending.values()) { clearTimeout(item.timer); item.reject(new Error("Connection lost before confirmation.")); }
    this.pending.clear();
  }
  private connect() {
    if (this.stopped) return;
    this.status(this.attempts ? "reconnecting" : "connecting");
    let ws: WebSocket;
    try { ws = new WebSocket(endpoints().ws); }
    catch (e) { this.status("offline"); this.failure(e instanceof Error ? e.message : "Unable to connect."); return; }
    this.socket = ws;
    this.handshake = setTimeout(() => ws.close(), 10000);
    ws.onmessage = event => {
      if (this.socket !== ws || this.stopped) return;
      try {
        const frame = JSON.parse(event.data);
        if (frame.v !== 1 || !frame.payload || typeof frame.type !== "string") throw new Error("Invalid server frame");
        if (frame.type === "request.ok" || frame.type === "request.error") {
          const item = this.pending.get(frame.requestId);
          if (!item) return;
          clearTimeout(item.timer); this.pending.delete(frame.requestId);
          if (frame.type === "request.error") item.reject(new CommandError(frame.payload.code, frame.payload.message));
          else item.resolve(frame.payload);
          return;
        }
        if (frame.type === "session.ready") {
          if (frame.payload.user.id !== this.userID) { this.stop(); this.status("expired"); return; }
          clearTimeout(this.handshake); this.ready = true; this.attempts = 0; this.status("connected");
        }
        this.event(frame as ServerEvent);
      } catch { this.failure("The server sent an invalid response."); ws.close(); }
    };
    ws.onclose = () => {
      if (this.socket !== ws || this.stopped) return;
      clearTimeout(this.handshake); this.ready = false; this.rejectPending();
      this.status("reconnecting");
      void this.checkAndRetry(ws);
    };
    ws.onerror = () => {};
  }
  private async checkAndRetry(ws: WebSocket) {
    try {
      const session = await authRequest<Session>("/auth/me");
      if (this.socket !== ws || this.stopped) return;
      if (session.user.id !== this.userID) { this.stop(); this.status("expired"); return; }
    } catch (e) {
      if (this.socket !== ws || this.stopped) return;
      if (e instanceof ApiError && e.status === 401) { this.stop(); this.status("expired"); return; }
    }
    if (this.socket !== ws || this.stopped) return;
    if (++this.attempts > 8) { this.status("offline"); return; }
    const delay = Math.min(500 * 2 ** (this.attempts - 1), 15000) + Math.random() * 250;
    this.retry = setTimeout(() => this.connect(), delay);
  }
}

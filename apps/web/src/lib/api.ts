export type User = { id: string; username: string };
export type Session = { user: User; expiresAt: string };

export class ApiError extends Error {
  constructor(public status: number, message: string) { super(message); }
}

export function endpoints() {
  const api = new URL(process.env.NEXT_PUBLIC_API_URL ?? "http://localhost:8080");
  const ws = new URL(process.env.NEXT_PUBLIC_WS_URL ?? "ws://localhost:8080/ws");
  if (!["http:", "https:"].includes(api.protocol) || !["ws:", "wss:"].includes(ws.protocol) ||
      api.hostname !== ws.hostname || api.username || api.password || ws.username || ws.password || ws.search) {
    throw new Error("Check the API and WebSocket URLs in the frontend configuration.");
  }
  if (typeof window !== "undefined" && api.hostname !== window.location.hostname) {
    throw new Error("The website and API must use the same hostname. Check NEXT_PUBLIC_API_URL and NEXT_PUBLIC_WS_URL.");
  }
  return { api, ws };
}

export async function authRequest<T>(path: string, body?: object): Promise<T> {
  const response = await fetch(new URL(path, endpoints().api), {
    method: body === undefined ? "GET" : "POST",
    credentials: "include", cache: "no-store", signal: AbortSignal.timeout(10000),
    ...(body === undefined ? {} : { headers: { "Content-Type": "application/json" }, body: JSON.stringify(body) }),
  });
  if (!response.ok) {
    const data = await response.json().catch(() => null);
    throw new ApiError(response.status, data?.error?.message ?? "The server could not complete this request.");
  }
  return response.status === 204 ? undefined as T : response.json();
}

export function errorText(error: unknown) {
  return error instanceof Error ? error.message : "Something went wrong. Please try again.";
}

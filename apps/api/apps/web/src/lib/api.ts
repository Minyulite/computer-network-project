const apiBaseUrl = process.env.API_BASE_URL ?? "http://localhost:8080";

export async function isApiHealthy(): Promise<boolean> {
  try {
    const response = await fetch(new URL("/health", apiBaseUrl), {
      cache: "no-store",
    });

    if (!response.ok) return false;

    const body: unknown = await response.json();
    return (
      typeof body === "object" &&
      body !== null &&
      "status" in body &&
      body.status === "ok"
    );
  } catch {
    return false;
  }
}

import { isApiHealthy } from "@/lib/api";

export default async function Home() {
  const healthy = await isApiHealthy();

  return (
    <main className="shell">
      <p className="eyebrow">PROJECT STARTER</p>
      <h1>Next.js frontend.<br />Go backend.</h1>
      <p className="intro">
        Two small applications in one readable repository, ready for your first feature.
      </p>

      <div className="status" role="status">
        <span className={`status-dot ${healthy ? "online" : "offline"}`} />
        <span>Go API {healthy ? "connected" : "unavailable"}</span>
      </div>

      <p className="hint">
        The frontend checks <code>GET /health</code> on the Go server.
        Start both apps using the commands in the root README.
      </p>
    </main>
  );
}

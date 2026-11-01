"use client";

import Link from "next/link";
import { useRouter } from "next/navigation";
import { useEffect, useState, type FormEvent } from "react";
import { authRequest, errorText } from "@/lib/api";

export function AuthForm({ mode }: { mode: "signin" | "signup" }) {
  const signup = mode === "signup";
  const router = useRouter();
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);
  useEffect(() => {
    let active = true;
    void authRequest("/auth/me").then(() => { if (active) router.replace("/"); }).catch(() => {});
    return () => { active = false; };
  }, [router]);

  async function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    const data = new FormData(event.currentTarget);
    const username = String(data.get("username")).trim().toLowerCase();
    const password = String(data.get("password"));
    if (!/^[a-z0-9_]{3,24}$/.test(username)) { setError("Use 3–24 letters, numbers, or underscores for your username."); return; }
    if (signup && (Array.from(password).length < 15 || Array.from(password).length > 128 || new TextEncoder().encode(password).length > 512)) {
      setError("Use a password with 15–128 characters (at most 512 UTF-8 bytes)."); return;
    }
    if (signup && password !== data.get("confirm")) { setError("Your passwords do not match."); return; }
    setBusy(true); setError("");
    try { await authRequest(`/auth/${signup ? "signup" : "login"}`, { username, password }); router.replace("/"); }
    catch (e) { setError(errorText(e)); setBusy(false); }
  }

  return <main className="auth-shell">
    <div className="auth-story">
      <Link className="brand" href="/">↗ Commonroom</Link>
      <div><p className="eyebrow">A PLACE TO CONNECT</p><h1>Good conversations<br />start here.</h1>
        <p>Catch up one to one, find your group, and keep the conversation going across your devices.</p></div>
      <p className="small">Your people. Your conversations.</p>
    </div>
    <section className="auth-panel" aria-labelledby="auth-title">
      <p className="eyebrow">COMMONROOM</p>
      <h2 id="auth-title">{signup ? "Make yourself at home." : "Welcome back."}</h2>
      <p className="muted">{signup ? "Create an account to join the conversation." : "Sign in to pick up where you left off."}</p>
      <form onSubmit={submit} className="stack">
        <label>Username<input name="username" autoComplete="username" required maxLength={24} autoCapitalize="none" spellCheck={false} aria-describedby="username-help" /></label>
        <span id="username-help" className="field-help">3–24 letters, numbers, or underscores. Names are lowercase.</span>
        <label>Password<input name="password" type="password" autoComplete={signup ? "new-password" : "current-password"} required aria-describedby={signup ? "password-help" : undefined} /></label>
        {signup && <><span id="password-help" className="field-help">15–128 characters. A memorable passphrase works well.</span>
          <label>Confirm password<input name="confirm" type="password" autoComplete="new-password" required /></label></>}
        {error && <p role="alert" className="error">{error}</p>}
        <button className="primary" disabled={busy}>{busy ? "Please wait…" : signup ? "Create account" : "Sign in"}</button>
      </form>
      <p className="auth-switch">{signup ? "Already have an account?" : "New here?"} <Link href={signup ? "/signin" : "/signup"}>{signup ? "Sign in" : "Create an account"}</Link></p>
    </section>
  </main>;
}

"use client";

import { useRouter } from "next/navigation";
import Link from "next/link";
import { useEffect, useState, type FormEvent } from "react";
import { Brand } from "@/components/icons";
import { authRequest, errorText } from "@/lib/api";

// What a group looks like once you are in: the same parts the app renders.
const preview = [
  { from: "mali", time: "7:02 PM", text: "Did anyone capture the handshake for lab 3?" },
  { from: "ton", time: "7:04 PM", text: "I have it. SYN, SYN-ACK, ACK in the first three frames" },
  { from: "You", time: "7:05 PM", text: "Can you post the file here? I'll write up the timing part", own: true },
];

function ConversationPreview() {
  return <figure className="preview" aria-hidden>
    <div className="preview-head">
      <span className="room-symbol">#</span>
      <span><strong>Network study</strong><small>3 members</small></span>
      <span className="preview-online"><i className="presence" />2 online</span>
    </div>
    <div className="preview-body">
      {preview.map(message => <div className={`message ${message.own ? "own" : ""}`} key={message.time}>
        <div className="message-meta"><strong>{message.from}</strong><time>{message.time}</time></div>
        <p>{message.text}</p>
      </div>)}
    </div>
  </figure>;
}

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
      <Brand href="/" />
      <div className="auth-story-copy">
        <h1>Private chats and open groups for everyone on your network</h1>
        <p>Talk one to one, join a group to follow what it is working on, or ask a shop&apos;s assistant a question. Your conversations follow you to every device.</p>
      </div>
      <ConversationPreview />
    </div>
    <section className="auth-panel" aria-labelledby="auth-title">
      <h2 id="auth-title">{signup ? "Create your account" : "Sign in to Commonroom"}</h2>
      <p className="muted">{signup ? "Pick the username people will see in your chats" : "Use the username and password you signed up with"}</p>
      <form onSubmit={submit} className="stack">
        <div className="field">
          <label>Username<input name="username" autoComplete="username" required maxLength={24} autoCapitalize="none" spellCheck={false} aria-describedby="username-help" /></label>
          <span id="username-help" className="field-help">3–24 letters, numbers, or underscores, all lowercase</span>
        </div>
        <div className="field">
          <label>Password<input name="password" type="password" autoComplete={signup ? "new-password" : "current-password"} required aria-describedby={signup ? "password-help" : undefined} /></label>
          {signup && <span id="password-help" className="field-help">15–128 characters. A memorable passphrase works well</span>}
        </div>
        {signup && <label>Confirm password<input name="confirm" type="password" autoComplete="new-password" required /></label>}
        {error && <p role="alert" className="error">{error}</p>}
        <button className="primary large" disabled={busy}>{busy ? (signup ? "Creating account…" : "Signing in…") : signup ? "Create account" : "Sign in"}</button>
      </form>
      <p className="auth-switch">{signup ? "Already have an account?" : "New here?"} <Link href={signup ? "/signin" : "/signup"}>{signup ? "Sign in" : "Create an account"}</Link></p>
    </section>
  </main>;
}

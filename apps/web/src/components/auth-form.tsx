"use client";

import { useRouter } from "next/navigation";
import Link from "next/link";
import { useEffect, useState, type FormEvent } from "react";
import { AvatarPicker } from "@/components/avatar";
import { Brand, Spinner, EyeIcon, EyeOffIcon } from "@/components/icons";
import { authRequest, errorText } from "@/lib/api";
import { personAvatars } from "@/lib/avatars";

// The toggle sits beside the label, not inside it, so the input keeps the plain label as its name.
function PasswordField({ label, name, autoComplete, help }: { label: string; name: string; autoComplete: string; help?: string }) {
  const [visible, setVisible] = useState(false);
  const helpId = `${name}-help`;
  return <div className="field">
    <div className="password-wrap">
      <label>{label}<input name={name} type={visible ? "text" : "password"} autoComplete={autoComplete} required
        autoCapitalize="none" spellCheck={false} aria-describedby={help ? helpId : undefined} /></label>
      <button type="button" className="reveal" aria-label={visible ? "Hide password" : "Show password"} aria-pressed={visible}
        onClick={() => setVisible(value => !value)}>{visible ? <EyeOffIcon /> : <EyeIcon />}</button>
    </div>
    {help && <span id={helpId} className="field-help">{help}</span>}
  </div>;
}

export function AuthForm({ mode }: { mode: "signin" | "signup" }) {
  const signup = mode === "signup";
  const router = useRouter();
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);
  const [avatar, setAvatar] = useState(""); // Empty lets the server choose one.
  useEffect(() => {
    let active = true;
    void authRequest("/auth/me").then(() => { if (active) router.replace("/"); }).catch(() => {});
    return () => { active = false; };
  }, [router]);

  async function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    const data = new FormData(event.currentTarget);
    // Same tidy-up as the server: spaces trimmed and collapsed, case kept.
    const username = String(data.get("username")).split(/\s+/).filter(Boolean).join(" ");
    const password = String(data.get("password"));
    if (!/^[\x20-\x7E]{3,24}$/.test(username) || !/[A-Za-z]/.test(username)) {
      setError("Use 3–24 English characters for your username, with at least one letter. Numbers and symbols are optional."); return;
    }
    if (signup && (Array.from(password).length < 8 || Array.from(password).length > 128 || new TextEncoder().encode(password).length > 512)) {
      setError("Use a password with 8–128 characters (at most 512 UTF-8 bytes)."); return;
    }
    if (signup && password !== data.get("confirm")) { setError("Your passwords do not match."); return; }
    setBusy(true); setError("");
    try { await authRequest(`/auth/${signup ? "signup" : "login"}`, { username, password, ...(signup && avatar ? { avatar } : {}) }); router.replace("/"); }
    catch (e) { setError(errorText(e)); setBusy(false); }
  }

  return <main className="auth-shell">
    <section className="auth-card" aria-labelledby="auth-title">
      <Brand href="/" />
      <h1 id="auth-title">{signup ? "Create your account" : "Sign in to Commonroom"}</h1>
      <form onSubmit={submit} className="stack">
        {signup && <AvatarPicker ids={personAvatars} name="avatar" label="Choose an avatar (optional)" value={avatar} onChange={setAvatar} />}
        <div className="field">
          <label>Username<input name="username" autoComplete="username" required spellCheck={false} /></label>
        </div>
        <PasswordField label="Password" name="password" autoComplete={signup ? "new-password" : "current-password"}
          help={signup ? "At least 8 characters" : undefined} />
        {signup && <PasswordField label="Confirm password" name="confirm" autoComplete="new-password" />}
        {error && <p role="alert" className="error">{error}</p>}
        <button className="primary large" disabled={busy}>{busy ? <Spinner size={18} label={signup ? "Creating account" : "Signing in"} /> : signup ? "Create account" : "Sign in"}</button>
      </form>
      <p className="auth-switch">{signup ? "Already have an account?" : "New here?"} <Link href={signup ? "/signin" : "/signup"}>{signup ? "Sign in" : "Create an account"}</Link></p>
    </section>
  </main>;
}

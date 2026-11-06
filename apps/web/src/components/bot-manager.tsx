"use client";

import { useState, type FormEvent } from "react";
import { errorText } from "@/lib/api";
import type { ChatStore } from "@/lib/chat/store";
import type { Bot, BotInput } from "@/lib/chat/types";

const limits = { bots: 5, templates: 20, name: 64, description: 2000, question: 300, answer: 1000 };
const emptyForm = (): BotInput => ({ name: "", description: "", enabled: true, templates: [] });
const count = (value: string) => Array.from(value.trim()).length;

type Props = { store: ChatStore; bots: Bot[]; connected: boolean; onClose: () => void; onOpenBot: (id: string) => void };

// Owner's view of their shop assistants: description and answer templates.
export function BotManager({ store, bots, connected, onClose, onOpenBot }: Props) {
  const [editing, setEditing] = useState<string | "new" | null>(null);
  const [form, setForm] = useState<BotInput>(emptyForm());
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");

  async function edit(id: string) {
    setBusy(true); setError("");
    try {
      const detail = await store.getBot(id);
      setForm({ name: detail.name, description: detail.description, enabled: detail.enabled, templates: detail.templates });
      setEditing(id);
    } catch (e) { setError(errorText(e)); } finally { setBusy(false); }
  }
  function startNew() { setForm(emptyForm()); setEditing("new"); setError(""); }
  function setTemplate(index: number, patch: Partial<BotInput["templates"][number]>) {
    setForm(value => ({ ...value, templates: value.templates.map((t, i) => i === index ? { ...t, ...patch } : t) }));
  }

  async function save(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    const templates = form.templates.map(t => ({ question: t.question.trim(), answer: t.answer.trim() }))
      .filter(t => t.question || t.answer);
    if (!count(form.name) || count(form.name) > limits.name) { setError("Give the assistant a name of 1–64 characters."); return; }
    if (count(form.description) > limits.description) { setError("The shop description is limited to 2,000 characters."); return; }
    if (templates.some(t => !t.question || !t.answer || Array.from(t.question).length > limits.question || Array.from(t.answer).length > limits.answer)) {
      setError("Every template needs a question (up to 300 characters) and an answer (up to 1,000)."); return;
    }
    setBusy(true); setError("");
    try {
      await store.saveBot(editing === "new" ? undefined : editing ?? undefined,
        { name: form.name.trim(), description: form.description.trim(), enabled: form.enabled, templates });
      setEditing(null);
    } catch (e) { setError(errorText(e)); } finally { setBusy(false); }
  }

  return <section className="bot-manager" aria-label="My shop assistants">
    <header className="bot-manager-head"><div><p className="eyebrow">YOUR SHOP</p><h1>Shop assistants</h1></div>
      <button type="button" onClick={onClose}>Back to chats</button></header>
    <p className="small muted bot-note">Customers chat with your assistant privately; you do not see their conversations. Their messages and your description are sent to Google Gemini to write answers. Replies to template questions are your own words, unchanged.</p>
    {error && <p className="error" role="alert">{error}</p>}

    {editing === null ? <>
      {!bots.length && <p className="muted">You have no assistants yet. Create one to answer customer questions automatically.</p>}
      <ul className="bot-list">{bots.map(bot => <li key={bot.id}>
        <span><strong>{bot.name}</strong><small>{bot.enabled ? "Live" : "Turned off"}</small></span>
        <span className="bot-actions">
          <button type="button" disabled={!connected || busy || !bot.enabled} onClick={() => onOpenBot(bot.id)}>Try it</button>
          <button type="button" disabled={!connected || busy} onClick={() => void edit(bot.id)}>Edit</button>
        </span></li>)}</ul>
      <button type="button" className="primary" disabled={!connected || busy || bots.length >= limits.bots} onClick={startNew}>New assistant</button>
      {bots.length >= limits.bots && <p className="small muted">You can have up to {limits.bots} assistants.</p>}
    </> : <form className="bot-form" onSubmit={save}>
      <label>Name<input value={form.name} maxLength={limits.name} required onChange={e => setForm({ ...form, name: e.target.value })} placeholder="e.g. Corner Café" /></label>
      <label>What is your shop about?
        <textarea rows={6} value={form.description} maxLength={limits.description} onChange={e => setForm({ ...form, description: e.target.value })}
          placeholder="What you sell, where you are, opening hours, prices, policies. The assistant answers only from this text and your templates." /></label>
      <small className="muted">{count(form.description)}/{limits.description}</small>

      <fieldset className="bot-templates"><legend>Answer templates <small>{form.templates.length}/{limits.templates}</small></legend>
        <p className="small muted">When a customer asks something similar to a question, the assistant sends your answer exactly as written.</p>
        {form.templates.map((t, i) => <div className="bot-template" key={i}>
          <label>Question<input value={t.question} maxLength={limits.question} onChange={e => setTemplate(i, { question: e.target.value })} placeholder="What time do you open?" /></label>
          <label>Answer<textarea rows={2} value={t.answer} maxLength={limits.answer} onChange={e => setTemplate(i, { answer: e.target.value })} placeholder="10.00" /></label>
          <button type="button" className="text-button" onClick={() => setForm(value => ({ ...value, templates: value.templates.filter((_, j) => j !== i) }))}>Remove</button>
        </div>)}
        <button type="button" disabled={form.templates.length >= limits.templates}
          onClick={() => setForm(value => ({ ...value, templates: [...value.templates, { question: "", answer: "" }] }))}>Add template</button>
      </fieldset>

      {editing !== "new" && <label className="bot-toggle"><input type="checkbox" checked={form.enabled} onChange={e => setForm({ ...form, enabled: e.target.checked })} /> Live (customers can find and chat with it)</label>}
      <div className="bot-actions">
        <button className="primary" disabled={!connected || busy}>{busy ? "Saving…" : "Save assistant"}</button>
        <button type="button" disabled={busy} onClick={() => { setEditing(null); setError(""); }}>Cancel</button>
      </div>
    </form>}
  </section>;
}

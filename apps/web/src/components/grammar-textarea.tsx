"use client";

import { useCallback, useEffect, useLayoutEffect, useRef, useState, type MouseEvent as ReactMouseEvent } from "react";
import { errorText } from "@/lib/api";
import { wordDiff } from "@/lib/word-diff";

type Selection = { start: number; end: number; text: string };
type Panel =
  | ({ kind: "menu" } & Selection)
  | ({ kind: "loading" } & Selection)
  | ({ kind: "preview"; corrected: string } & Selection)
  | { kind: "nochange" }
  | { kind: "error"; message: string };

type Props = {
  id: string;
  value: string;
  onChange: (value: string) => void;
  correct: (text: string, before: string, after: string) => Promise<string>;
  canCorrect: boolean;
  onConvert?: (textarea: HTMLTextAreaElement) => void;
  placeholder?: string;
  rows?: number;
};

// Message box with a custom right-click menu: select text, choose "Correct
// grammar", review the diff, and Accept to replace only the selection.
export function GrammarTextarea({ id, value, onChange, correct, canCorrect, onConvert, placeholder, rows }: Props) {
  const textarea = useRef<HTMLTextAreaElement>(null);
  const panelRef = useRef<HTMLDivElement>(null);
  const [panel, setPanel] = useState<Panel | null>(null);
  const [anchor, setAnchor] = useState({ x: 0, y: 0 });
  // Bumped whenever the panel closes, so a late reply to an abandoned request is ignored.
  const requestToken = useRef(0);
  const pendingSelection = useRef<{ start: number; end: number } | null>(null);

  const close = useCallback(() => {
    requestToken.current++;
    setPanel(null);
  }, []);

  function openMenu(event: ReactMouseEvent<HTMLTextAreaElement>) {
    const { selectionStart: start, selectionEnd: end, value: current } = event.currentTarget;
    const text = current.slice(start, end);
    if (start === end || !text.trim()) return; // Nothing selected: keep the browser's own menu.
    event.preventDefault();
    requestToken.current++;
    setAnchor({ x: event.clientX, y: event.clientY });
    setPanel({ kind: "menu", start, end, text });
  }

  async function runCorrection({ start, end, text }: Selection) {
    const token = ++requestToken.current;
    setPanel({ kind: "loading", start, end, text });
    try {
      const corrected = await correct(text, value.slice(0, start), value.slice(end));
      if (token !== requestToken.current) return;
      if (corrected.trim() === text.trim()) {
        setPanel({ kind: "nochange" });
        setTimeout(() => { if (token === requestToken.current) setPanel(null); }, 1500);
      } else setPanel({ kind: "preview", start, end, text, corrected });
    } catch (error) {
      if (token === requestToken.current) setPanel({ kind: "error", message: errorText(error) });
    }
  }

  function accept({ start, end, text, corrected }: Extract<Panel, { kind: "preview" }>) {
    // The draft may have changed while waiting. Relocate the text only when
    // there is exactly one place it could be, so a repeat is never replaced by mistake.
    if (value.slice(start, end) !== text) {
      start = value.indexOf(text);
      if (start === -1 || start !== value.lastIndexOf(text)) {
        setPanel({ kind: "error", message: "The selected text changed. Select it again." });
        return;
      }
      end = start + text.length;
    }
    pendingSelection.current = { start, end: start + corrected.length };
    onChange(value.slice(0, start) + corrected + value.slice(end));
    close();
  }

  // Re-select the corrected text once the new value has rendered.
  useLayoutEffect(() => {
    const selection = pendingSelection.current;
    if (selection && textarea.current) {
      textarea.current.focus();
      textarea.current.setSelectionRange(selection.start, selection.end);
      pendingSelection.current = null;
    }
  }, [value]);

  useEffect(() => {
    if (!panel) return;
    const onMouseDown = (event: MouseEvent) => {
      if (!panelRef.current?.contains(event.target as Node)) close();
    };
    const onKeyDown = (event: KeyboardEvent) => { if (event.key === "Escape") close(); };
    // Only movement of the box or page detaches the panel. Other scrolling must
    // not close it: the message list scrolls by itself on every incoming
    // message, and the panel's own diff can scroll too.
    const onScroll = (event: Event) => {
      if (event.target === textarea.current || event.target === document) close();
    };
    document.addEventListener("mousedown", onMouseDown);
    document.addEventListener("keydown", onKeyDown);
    window.addEventListener("resize", close);
    window.addEventListener("scroll", onScroll, true);
    return () => {
      document.removeEventListener("mousedown", onMouseDown);
      document.removeEventListener("keydown", onKeyDown);
      window.removeEventListener("resize", close);
      window.removeEventListener("scroll", onScroll, true);
    };
  }, [panel, close]);

  // Keep the panel on screen; it may open near the bottom-right edge.
  useLayoutEffect(() => {
    const element = panelRef.current;
    if (!panel || !element) return;
    const { offsetHeight: height, offsetWidth: width } = element;
    element.style.left = `${Math.max(8, Math.min(anchor.x, window.innerWidth - width - 8))}px`;
    element.style.top = `${anchor.y + height + 8 > window.innerHeight ? Math.max(8, anchor.y - height) : anchor.y}px`;
  }, [panel, anchor]);

  // Every button here sits inside the composer form and needs type="button",
  // or clicking it would submit the form and send the draft.
  return <>
    <textarea ref={textarea} id={id} placeholder={placeholder} value={value} rows={rows}
      onChange={event => onChange(event.target.value)}
      onContextMenu={openMenu}
      onKeyDown={event => {
        if (onConvert && event.ctrlKey && event.key === "Shift") {
          event.preventDefault();
          onConvert(event.currentTarget);
          return;
        }
        if (event.key === "Enter" && !event.shiftKey && !event.nativeEvent.isComposing) {
          event.preventDefault();
          event.currentTarget.form?.requestSubmit();
        }
      }} />
    {panel && <div ref={panelRef} className={panel.kind === "menu" ? "grammar-menu" : "grammar-panel"} role="dialog" aria-label="Grammar correction">
      {panel.kind === "menu" && <button type="button" className="grammar-item" disabled={!canCorrect}
        title={canCorrect ? "Sends the selection and nearby draft text to Google Gemini" : "Connection unavailable"}
        onClick={() => void runCorrection(panel)}><span aria-hidden>✨</span> Correct grammar</button>}
      {panel.kind === "loading" && <div className="grammar-status" role="status"><span className="grammar-spinner" aria-hidden /> Checking grammar with Gemini…</div>}
      {panel.kind === "nochange" && <div className="grammar-status ok" role="status">✓ No issues found</div>}
      {panel.kind === "error" && <>
        <div className="grammar-status error" role="alert">{panel.message}</div>
        <div className="grammar-actions"><button type="button" onClick={close}>Close</button></div>
      </>}
      {panel.kind === "preview" && <>
        <p className="eyebrow">SUGGESTED CORRECTION</p>
        <div className="grammar-diff">{wordDiff(panel.text, panel.corrected).map((part, index) => <span key={index} className={`diff-${part.type}`}>{part.text}</span>)}</div>
        <div className="grammar-actions">
          <button type="button" onClick={close}>Cancel</button>
          <button type="button" className="primary" onClick={() => accept(panel)} autoFocus>Accept</button>
        </div>
      </>}
    </div>}
  </>;
}

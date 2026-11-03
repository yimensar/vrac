import { useState, type FormEvent } from "react";
import { ArrowRight, ArrowUpRight, LoaderCircle } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Brand } from "@/components/Brand";
import { api, type Session } from "@/lib/api";
export function Auth({
  session,
  onAuthenticated,
}: {
  session: Session;
  onAuthenticated: () => void;
}) {
  const [password, setPassword] = useState(""),
    [busy, setBusy] = useState(false),
    [error, setError] = useState("");
  async function submit(event: FormEvent) {
    event.preventDefault();
    setBusy(true);
    setError("");
    try {
      await api(session.needsSetup ? "/setup" : "/login", {
        method: "POST",
        body: JSON.stringify({ password }),
      });
      onAuthenticated();
    } catch (e) {
      setError((e as Error).message);
    } finally {
      setBusy(false);
    }
  }
  return (
    <main className="auth-page">
      <div className="auth-art" aria-hidden="true">
        <span className="orbit orbit-one" />
        <span className="orbit orbit-two" />
        <span className="orbit-dot" />
        <div className="auth-art-copy">
          <span className="eyebrow">UN PEU DE PLACE POUR VOS IDÉES</span>
          <h1>
            Tout noter.
            <br />
            Retrouver le fil.
          </h1>
          <p>Les idées passent. Les bonnes connexions restent.</p>
        </div>
        <span className="auth-art-foot">Votre mémoire, à votre rythme.</span>
      </div>
      <div className="auth-form-wrap">
        <Brand />
        <div className="auth-form">
          <span className="eyebrow">VOTRE ESPACE PERSONNEL</span>
          <h2>
            {session.needsSetup
              ? "Bienvenue chez vous."
              : "Heureux de vous retrouver."}
          </h2>
          <p>
            {session.needsSetup
              ? "Choisissez un mot de passe pour ouvrir votre espace et protéger vos notes."
              : "Un espace pour vos idées, vos découvertes et tout ce qui mérite de rester."}
          </p>
          <form onSubmit={submit}>
            <label htmlFor="password">
              {session.needsSetup ? "Créer votre mot de passe" : "Mot de passe"}
            </label>
            <Input
              id="password"
              type="password"
              autoComplete={
                session.needsSetup ? "new-password" : "current-password"
              }
              value={password}
              onChange={(e) => setPassword(e.target.value)}
              required
              minLength={session.needsSetup ? 12 : 1}
              autoFocus
              placeholder={
                session.needsSetup
                  ? "12 caractères minimum"
                  : "Votre mot de passe"
              }
            />
            {error && (
              <p className="form-error" role="alert">
                {error}
              </p>
            )}
            <Button type="submit" className="auth-submit" disabled={busy}>
              {busy ? <LoaderCircle className="animate-spin" /> : null}
              {session.needsSetup ? "Créer mon espace" : "Ouvrir mon espace"}
              <ArrowRight />
            </Button>
          </form>
          <div className="auth-private">
            <span className="status-dot" /> Vos notes restent sur votre serveur.
          </div>
        </div>
        <a
          className="source-link"
          href="https://github.com/yimensar/vrac"
          target="_blank"
          rel="noreferrer"
        >
          Vrac est un logiciel libre <ArrowUpRight size={13} />
        </a>
      </div>
    </main>
  );
}

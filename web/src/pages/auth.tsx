import { useState, type FormEvent } from "react"
import { useQuery } from "@tanstack/react-query"
import { api } from "@/lib/api"
import { reportError, useRefresh } from "@/lib/hooks"
import { ceremony, deviceName, passkeysSupported } from "@/lib/webauthn"
import { Button } from "@/components/ui/button"
import { Card, CardContent, CardDescription, CardFooter, CardHeader, CardTitle } from "@/components/ui/card"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"

/** Signs in, or creates the owner account on a new instance: username and
 * password, then a passkey; or the identity provider when configured. */
export function SignIn({ owner, provider }: { owner: boolean; provider: string }) {
  const refresh = useRefresh()
  const [username, setUsername] = useState("")
  const [password, setPassword] = useState("")
  const [confirm, setConfirm] = useState("")
  const [step, setStep] = useState<"form" | "passkey">("form")
  const [error, setError] = useState("")
  const creating = !owner
  const submit = async (e: FormEvent) => {
    e.preventDefault()
    setError("")
    if (creating && password !== confirm) return setError("The passwords differ.")
    if (!passkeysSupported()) return setError("This browser does not support passkeys.")
    try {
      const c = await (creating ? api.setupStart(username, password) : api.loginStart(username, password))
      setStep("passkey")
      const credential = await ceremony(c.mode, c.options)
      await api.signinFinish(c.flow, credential, deviceName())
      await refresh()
    } catch (err) {
      setStep("form")
      if (err instanceof DOMException) setError("The passkey was cancelled or refused.")
      else reportError(err)
    }
  }
  const back = encodeURIComponent(window.location.pathname + window.location.search)
  return (
    <div className="flex min-h-svh items-center justify-center p-4">
      <Card className="w-full max-w-sm">
        <CardHeader>
          <CardTitle>{creating ? "Create your vrac" : "Sign in to vrac"}</CardTitle>
          <CardDescription>
            {step === "passkey"
              ? "Confirm with your passkey."
              : creating
                ? "Choose a username and a password; a passkey will then protect the account."
                : "Your username and password, then your passkey."}
          </CardDescription>
        </CardHeader>
        <CardContent className="mt-6 space-y-4">
          {provider && (
            <>
              <Button asChild variant="outline" className="w-full">
                <a href={`/auth/oidc/start?return=${back}`}>Continue with {provider}</a>
              </Button>
              <div className="flex items-center gap-3 text-xs text-muted-foreground">
                <span className="h-px flex-1 bg-border" />
                or
                <span className="h-px flex-1 bg-border" />
              </div>
            </>
          )}
          <form onSubmit={submit} className="space-y-3">
            <div className="space-y-1.5">
              <Label htmlFor="username">Username</Label>
              <Input id="username" autoFocus autoComplete="username" value={username} onChange={(e) => setUsername(e.target.value)} />
            </div>
            <div className="space-y-1.5">
              <Label htmlFor="password">Password</Label>
              <Input id="password" type="password" autoComplete={creating ? "new-password" : "current-password"} value={password} onChange={(e) => setPassword(e.target.value)} />
            </div>
            {creating && (
              <div className="space-y-1.5">
                <Label htmlFor="confirm">Confirm the password</Label>
                <Input id="confirm" type="password" autoComplete="new-password" value={confirm} onChange={(e) => setConfirm(e.target.value)} />
              </div>
            )}
            {error && <p className="text-sm text-destructive">{error}</p>}
            <Button type="submit" className="w-full" disabled={step === "passkey" || !username || password.length < (creating ? 10 : 1)}>
              {creating ? "Create the account and its passkey" : "Continue"}
            </Button>
          </form>
        </CardContent>
      </Card>
    </div>
  )
}

export function AuthorizePage() {
  const query = window.location.search.slice(1)
  const { data, error } = useQuery({ queryKey: ["oauth", query], queryFn: () => api.oauthRequest(query), retry: false })
  const decide = async (approve: boolean) => {
    try {
      window.location.href = (await api.oauthDecision(query, approve)).redirect
    } catch (err) {
      reportError(err)
    }
  }
  return (
    <div className="flex min-h-[60svh] items-center justify-center">
      <Card className="w-full max-w-md">
        <CardHeader>
          <CardTitle>{data ? `Connect ${data.client_name}?` : "Connect an application"}</CardTitle>
          <CardDescription>
            {error
              ? (error as Error).message
              : data && (
                  <>
                    <strong>{data.client_name}</strong> ({data.redirect_host}) will read and write your whole vrac on your behalf. You can revoke it in Settings.
                  </>
                )}
          </CardDescription>
        </CardHeader>
        {data && (
          <CardFooter className="mt-6 justify-end gap-2">
            <Button variant="outline" onClick={() => decide(false)}>
              Deny
            </Button>
            <Button onClick={() => decide(true)}>Allow</Button>
          </CardFooter>
        )}
      </Card>
    </div>
  )
}


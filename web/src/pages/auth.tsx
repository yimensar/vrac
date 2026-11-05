import { useState, type FormEvent } from "react"
import { useQuery } from "@tanstack/react-query"
import { api } from "@/lib/api"
import { reportError, useRefresh } from "@/lib/hooks"
import { Button } from "@/components/ui/button"
import { Card, CardContent, CardDescription, CardFooter, CardHeader, CardTitle } from "@/components/ui/card"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"

export function SignIn({ owner }: { owner: boolean }) {
  const refresh = useRefresh()
  const [password, setPassword] = useState("")
  const [busy, setBusy] = useState(false)
  const submit = async (e: FormEvent) => {
    e.preventDefault()
    setBusy(true)
    try {
      await (owner ? api.login(password) : api.setup(password))
      await refresh()
    } catch (err) {
      reportError(err)
    } finally {
      setBusy(false)
    }
  }
  return (
    <div className="flex min-h-svh items-center justify-center p-4">
      <Card className="w-full max-w-sm">
        <form onSubmit={submit}>
          <CardHeader>
            <CardTitle>{owner ? "Sign in to vrac" : "Welcome to vrac"}</CardTitle>
            <CardDescription>{owner ? "Enter your password." : "Choose the password of this instance (10 characters or more)."}</CardDescription>
          </CardHeader>
          <CardContent className="my-6 space-y-2">
            <Label htmlFor="password">Password</Label>
            <Input id="password" type="password" autoFocus autoComplete={owner ? "current-password" : "new-password"} value={password} onChange={(e) => setPassword(e.target.value)} />
          </CardContent>
          <CardFooter>
            <Button type="submit" className="w-full" disabled={busy || (!owner && password.length < 10)}>
              {owner ? "Sign in" : "Create my vrac"}
            </Button>
          </CardFooter>
        </form>
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


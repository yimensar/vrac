// Browser side of passkey ceremonies. The server sends WebAuthn options as
// JSON with base64url fields; the browser API wants binary fields and
// answers with binary fields. Modern browsers convert both ways; older ones
// use the conversions below.

type JSONOptions = { publicKey: Record<string, any> }

const decode = (s: string) => Uint8Array.from(atob(s.replace(/-/g, "+").replace(/_/g, "/")), (c) => c.charCodeAt(0))
const encode = (b: ArrayBuffer | null) =>
  b ? btoa(String.fromCharCode(...new Uint8Array(b))).replace(/\+/g, "-").replace(/\//g, "_").replace(/=+$/, "") : null

type Converters = {
  parseCreationOptionsFromJSON?: (o: unknown) => PublicKeyCredentialCreationOptions
  parseRequestOptionsFromJSON?: (o: unknown) => PublicKeyCredentialRequestOptions
}
// Looked up on use: the browser API may appear after this module loads.
const pkc = () => (typeof PublicKeyCredential !== "undefined" ? (PublicKeyCredential as unknown as Converters) : undefined)

export const passkeysSupported = () => pkc() !== undefined && typeof navigator.credentials?.create === "function"

function creationOptions(o: Record<string, any>): PublicKeyCredentialCreationOptions {
  const p = pkc()
  if (p?.parseCreationOptionsFromJSON) return p.parseCreationOptionsFromJSON(o)
  return {
    ...o,
    challenge: decode(o.challenge),
    user: { ...o.user, id: decode(o.user.id) },
    excludeCredentials: (o.excludeCredentials ?? []).map((c: any) => ({ ...c, id: decode(c.id) })),
  } as unknown as PublicKeyCredentialCreationOptions
}

function requestOptions(o: Record<string, any>): PublicKeyCredentialRequestOptions {
  const p = pkc()
  if (p?.parseRequestOptionsFromJSON) return p.parseRequestOptionsFromJSON(o)
  return {
    ...o,
    challenge: decode(o.challenge),
    allowCredentials: (o.allowCredentials ?? []).map((c: any) => ({ ...c, id: decode(c.id) })),
  } as unknown as PublicKeyCredentialRequestOptions
}

function toJSON(c: PublicKeyCredential): unknown {
  const native = (c as unknown as { toJSON?: () => unknown }).toJSON
  if (native) return native.call(c)
  const r = c.response as AuthenticatorAttestationResponse & AuthenticatorAssertionResponse
  const response: Record<string, string | null> = { clientDataJSON: encode(r.clientDataJSON) }
  if ("attestationObject" in r) response.attestationObject = encode(r.attestationObject)
  if ("authenticatorData" in r) {
    response.authenticatorData = encode(r.authenticatorData)
    response.signature = encode(r.signature)
    response.userHandle = encode(r.userHandle)
  }
  return { id: c.id, rawId: encode(c.rawId), type: c.type, response, clientExtensionResults: c.getClientExtensionResults() }
}

/** Runs a ceremony the server started and returns the credential as JSON. */
export async function ceremony(mode: "create" | "get", options: JSONOptions): Promise<unknown> {
  const credential =
    mode === "create"
      ? await navigator.credentials.create({ publicKey: creationOptions(options.publicKey) })
      : await navigator.credentials.get({ publicKey: requestOptions(options.publicKey) })
  if (!credential) throw new Error("No passkey was provided.")
  return toJSON(credential as PublicKeyCredential)
}

/** A readable default name for a new passkey. */
export function deviceName(): string {
  const ua = navigator.userAgent
  if (/iPhone/.test(ua)) return "iPhone"
  if (/iPad/.test(ua)) return "iPad"
  if (/Android/.test(ua)) return "Android"
  if (/Mac OS X/.test(ua)) return "Mac"
  if (/Windows/.test(ua)) return "Windows"
  return "Passkey"
}

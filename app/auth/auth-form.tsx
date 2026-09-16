"use client";

import { useState, type FormEvent } from "react";
import { useRouter } from "next/navigation";
import styles from "./auth.module.css";
import { APIError, apiRequest, type SignupResponse, type User } from "../../lib/api";

type Mode = "login" | "signup";
type Errors = Partial<Record<"name" | "email" | "password" | "organization", string>>;

export function AuthForm({ mode }: { mode: Mode }) {
  const router = useRouter();
  const signup = mode === "signup"; const [name, setName] = useState(""); const [email, setEmail] = useState(""); const [password, setPassword] = useState(""); const [organization, setOrganization] = useState(""); const [errors, setErrors] = useState<Errors>({}); const [message, setMessage] = useState(""); const [submitting,setSubmitting]=useState(false);
  async function submit(event: FormEvent) { event.preventDefault(); const next: Errors = {}; if (signup && !name.trim()) next.name = "Enter your name."; if (!email.trim()) next.email = "Enter your email."; else if (!/^\S+@\S+\.\S+$/.test(email)) next.email = "Enter a valid email address."; if (!password) next.password = signup ? "Create a password." : "Enter your password."; else if (signup && new TextEncoder().encode(password).length < 12) next.password = "Password must be at least 12 characters."; if(signup&&!organization.trim())next.organization="Enter your organization."; setErrors(next); if (Object.keys(next).length) { setMessage(""); return; }
    setSubmitting(true);setMessage("");try{if(signup){const organizationName=organization.trim();await apiRequest<SignupResponse>("/v1/auth/signup",{method:"POST",body:JSON.stringify({full_name:name.trim(),email:email.trim(),password,organization:{name:organizationName},workspace:{name:`${organizationName} Infrastructure`}})});router.push("/onboarding")}else{await apiRequest<{user:User}>("/v1/auth/login",{method:"POST",body:JSON.stringify({email:email.trim(),password})});router.push("/workspace");router.refresh()}}catch(reason){if(reason instanceof APIError){if(reason.code==="invalid_credentials")setMessage("Invalid email or password.");else if(reason.status===409)setMessage("An account or workspace with these details already exists.");else setMessage(reason.message)}else setMessage("We couldn't reach the service. Try again.")}finally{setSubmitting(false)}}
  return <form className={styles.form} onSubmit={submit} noValidate>
    {signup && <div className={styles.field}><label htmlFor="name">Full name</label><input id="name" autoComplete="name" placeholder="Your name" value={name} onChange={(e) => setName(e.target.value)} aria-invalid={Boolean(errors.name)} aria-describedby={errors.name ? "name-error" : undefined}/>{errors.name && <span className={styles.error} id="name-error">{errors.name}</span>}</div>}
    <div className={styles.field}><label htmlFor="email">{signup ? "Work email" : "Email"}</label><input id="email" type="email" inputMode="email" autoComplete="email" placeholder="you@company.com" value={email} onChange={(e) => setEmail(e.target.value)} aria-invalid={Boolean(errors.email)} aria-describedby={errors.email ? "email-error" : undefined}/>{errors.email && <span className={styles.error} id="email-error">{errors.email}</span>}</div>
    <div className={styles.field}><div className={styles.labelRow}><label htmlFor="password">Password</label>{!signup && <span>Forgot password?</span>}</div><input id="password" type="password" autoComplete={signup ? "new-password" : "current-password"} placeholder={signup ? "Create a password" : "Enter your password"} value={password} onChange={(e) => setPassword(e.target.value)} aria-invalid={Boolean(errors.password)} aria-describedby={errors.password ? "password-error" : undefined}/>{errors.password && <span className={styles.error} id="password-error">{errors.password}</span>}</div>
    {signup && <div className={styles.field}><label htmlFor="organization">Organization / company</label><input id="organization" autoComplete="organization" placeholder="Acme" value={organization} onChange={(e) => setOrganization(e.target.value)} aria-invalid={Boolean(errors.organization)} aria-describedby={errors.organization?"organization-error":undefined}/>{errors.organization&&<span className={styles.error} id="organization-error">{errors.organization}</span>}</div>}
    <button className={styles.primaryButton} type="submit" disabled={submitting}>{submitting?(signup?"Creating account…":"Signing in…"):(signup ? "Create account" : "Sign in")}</button>
    <div className={styles.separator}><span>or</span></div><button className={styles.githubButton} type="button" disabled aria-disabled="true" title="GitHub authentication is not available yet">Continue with GitHub</button>
    {signup && <p className={styles.legal}>By continuing, you agree to the service terms and privacy policy.</p>}
    {message && <p className={styles.success} role="alert">{message}</p>}
    <p className={styles.bottomPrompt}>{signup ? "Already have an account?" : "Don't have an account?"} <a href={signup ? "/login" : "/signup"}>{signup ? "Sign in" : "Create one"}</a></p>
  </form>;
}

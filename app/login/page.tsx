import type { Metadata } from "next";
import { AuthForm } from "../auth/auth-form";
import { AuthShell } from "../auth/auth-shell";
import styles from "../auth/auth.module.css";

export const metadata: Metadata = { title: "Sign in — Evalplane", description: "Sign in to your Evalplane workspace." };
export default function LoginPage() { return <AuthShell mode="login"><section className={styles.content}><div className={styles.intro}><p>WELCOME BACK</p><h1>Sign in</h1><span>Continue to your agents, trials, and evidence.</span></div><AuthForm mode="login"/></section></AuthShell>; }

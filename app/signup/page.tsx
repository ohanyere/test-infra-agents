import type { Metadata } from "next";
import { AuthForm } from "../auth/auth-form";
import { AuthShell } from "../auth/auth-shell";
import styles from "../auth/auth.module.css";

export const metadata: Metadata = { title: "Create your workspace — Evalplane", description: "Create an Evalplane workspace." };
export default function SignupPage() { return <AuthShell mode="signup"><section className={styles.content}><div className={styles.intro}><p>GET STARTED</p><h1>Create your workspace.</h1><span>Start testing infrastructure agents against realistic or custom scenarios.</span></div><AuthForm mode="signup"/></section></AuthShell>; }

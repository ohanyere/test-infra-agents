import type { Metadata } from "next";
import { AuthForm } from "../auth/auth-form";
import { AuthShell } from "../auth/auth-shell";
import styles from "../auth/auth.module.css";

export const metadata: Metadata = { title: "Create your workspace — Evalplane", description: "Create an Evalplane workspace." };
export default function SignupPage() { return <AuthShell mode="signup"><section className={styles.content}><div className={styles.intro}><p>TRY NOW</p><h1>Create your workspace</h1><span>Connect an agent and run your first qualification.</span></div><AuthForm mode="signup"/></section></AuthShell>; }

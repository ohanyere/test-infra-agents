import type { ReactNode } from "react";
import styles from "./auth.module.css";

export function AuthShell({ mode, children }: { mode: "login" | "signup"; children: ReactNode }) {
  const login = mode === "login";
  return <main className={styles.page}><header className={styles.header}><a className={styles.wordmark} href="/" aria-label="Evalplane home">evalplane</a><p>{login ? "Don't have an account?" : "Already have an account?"} <a href={login ? "/signup" : "/login"}>{login ? "Sign up" : "Sign in"}</a></p></header>{children}</main>;
}

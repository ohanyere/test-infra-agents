"use client";
import { useState } from "react";
import { useAuth } from "../../lib/auth";
import styles from "./onboarding.module.css";
import authStyles from "./onboarding-auth.module.css";
export function OnboardingAuthHeader(){const{logout}=useAuth();const[busy,setBusy]=useState(false);return <header className={styles.header}><a className={styles.wordmark} href="/">evalplane</a><button className={authStyles.signOut} type="button" disabled={busy} onClick={async()=>{setBusy(true);try{await logout()}catch{setBusy(false)}}}>{busy?"Signing out…":"Sign out"}</button></header>}

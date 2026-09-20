"use client";
import type { ReactNode } from "react";
import { useState } from "react";
import { usePathname } from "next/navigation";
import { useAuth } from "../../lib/auth";
import styles from "./workspace.module.css";
import authStyles from "./workspace-auth.module.css";

const navigation=[{label:"Overview",href:"/workspace"},{label:"Agents",href:"/agents"},{label:"Verifications",href:"/verifications"},{label:"Integrations",href:"/integrations"},{label:"Settings",href:"/workspace/settings"}];
function NavigationLinks(){const pathname=usePathname();return <nav className={styles.primaryNav} aria-label="Workspace navigation">{navigation.map((item)=>{const active=item.href==="/workspace"?pathname===item.href:pathname.startsWith(item.href);return <a className={active?styles.active:undefined} href={item.href} aria-current={active?"page":undefined} key={item.label}>{item.label}</a>})}</nav>}
function WorkspaceChrome({children}:{children:ReactNode}){const{data,workspace,loading,error,retry,logout}=useAuth();const[signingOut,setSigningOut]=useState(false);async function signOut(){setSigningOut(true);try{await logout()}catch{setSigningOut(false)}}
  if(loading)return <main className={authStyles.state} role="status">Loading workspace…</main>;
  if(error)return <main className={authStyles.state} role="alert"><p>{error}</p><button type="button" onClick={retry}>Retry</button></main>;
  if(!data||!workspace)return <main className={authStyles.state} role="alert">No workspace is available for this account.</main>;
  const user=data.user;const signOutControl=<button className={authStyles.signOut} type="button" disabled={signingOut} onClick={signOut}>{signingOut?"Signing out…":"Sign out"}</button>;
  return <div className={styles.appShell}><aside className={styles.sidebar}><a className={styles.wordmark} href="/">evalplane</a><div className={styles.workspaceIdentity}><strong>{workspace.name}</strong><span>{workspace.role} · Workspace</span></div><NavigationLinks/><div className={styles.user}><strong>{user.full_name}</strong><span>{user.email}</span>{signOutControl}</div></aside><header className={styles.mobileHeader}><a className={styles.wordmark} href="/">evalplane</a><span>{workspace.name}</span><details><summary aria-label="Open workspace navigation"><i/><i/></summary><div className={styles.mobileMenu}><NavigationLinks/><div className={styles.mobileUser}><strong>{user.full_name}</strong><span>{user.email}</span>{signOutControl}</div></div></details></header><div className={styles.main}>{children}</div></div>}
export default function WorkspaceLayout({children}:{children:ReactNode}){return <WorkspaceChrome>{children}</WorkspaceChrome>}

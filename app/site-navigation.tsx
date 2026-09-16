import { productName } from "./brand";
import styles from "./site-navigation.module.css";

export function PublicNavigation({ active }: { active?: "tasks" | "pricing" }) {
  return <header className={styles.header}><nav className={styles.nav} aria-label="Main navigation">
    <a className={styles.wordmark} href="/" aria-label={`${productName} home`}>{productName}</a>
    <div className={styles.links}><a href="/#product">Product</a><a className={active === "tasks" ? styles.active : undefined} href="/scenarios">Tasks</a><a className={active === "pricing" ? styles.active : undefined} href="/pricing">Pricing</a></div>
    <div className={styles.actions}><a href="/login">Sign in</a><a className={styles.primary} href="/signup">Try now</a></div>
    <details className={styles.mobile}><summary aria-label="Open navigation"><span/><span/></summary><div><a href="/#product">Product</a><a href="/scenarios">Tasks</a><a href="/pricing">Pricing</a><a href="/login">Sign in</a><a className={styles.primary} href="/signup">Try now</a></div></details>
  </nav></header>;
}

export function ProductNavigation({ active }: { active?: "overview" | "agents" | "qualification" | "tasks" | "activity" | "billing" }) {
  const links = [{key:"overview",label:"Overview",href:"/workspace"},{key:"agents",label:"Agents",href:"/agents"},{key:"qualification",label:"Qualification",href:"/qualification"},{key:"tasks",label:"Tasks",href:"/workspace/tasks"},{key:"activity",label:"Activity",href:"/runs"},{key:"billing",label:"Billing",href:"/workspace/billing"}];
  return <header className={`${styles.header} ${styles.productHeader}`}><nav className={styles.nav} aria-label="Product navigation">
    <a className={styles.wordmark} href="/workspace" aria-label={`${productName} workspace`}>{productName}</a>
    <div className={styles.productLinks}>{links.map(link=><a className={active===link.key?styles.active:undefined} href={link.href} key={link.key}>{link.label}</a>)}</div>
    <a className={styles.workspaceLink} href="/workspace/settings">Settings</a>
    <details className={styles.mobile}><summary aria-label="Open product navigation"><span/><span/></summary><div>{links.map(link=><a href={link.href} key={link.key}>{link.label}</a>)}<a href="/workspace/settings">Settings</a></div></details>
  </nav></header>;
}

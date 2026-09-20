import { productName } from "./brand";
import styles from "./site-navigation.module.css";

export function PublicNavigation({ active }: { active?: "docs" }) {
  return <header className={styles.header}><nav className={styles.nav} aria-label="Main navigation">
    <a className={styles.wordmark} href="/" aria-label={`${productName} home`}>{productName}</a>
    <div className={styles.links}><a href="/#product">Product</a><a href="/#verification-model">How it works</a><a className={active === "docs" ? styles.active : undefined} href="/#docs">Docs</a></div>
    <div className={styles.actions}><a href="/login">Sign in</a><a className={styles.primary} href="/signup">Get started</a></div>
    <details className={styles.mobile}><summary aria-label="Open navigation"><span/><span/></summary><div><a href="/#product">Product</a><a href="/#verification-model">How it works</a><a href="/#docs">Docs</a><a href="/login">Sign in</a><a className={styles.primary} href="/signup">Get started</a></div></details>
  </nav></header>;
}

export function ProductNavigation({ active }: { active?: "overview" | "agents" | "verifications" | "integrations" }) {
  // Primary product surfaces should follow docs/PRODUCT_DOCTRINE.md.
  const links = [{key:"overview",label:"Overview",href:"/workspace"},{key:"agents",label:"Agents",href:"/agents"},{key:"verifications",label:"Verifications",href:"/verifications"},{key:"integrations",label:"Integrations",href:"/integrations"}];
  return <header className={`${styles.header} ${styles.productHeader}`}><nav className={styles.nav} aria-label="Product navigation">
    <a className={styles.wordmark} href="/workspace" aria-label={`${productName} workspace`}>{productName}</a>
    <div className={styles.productLinks}>{links.map(link=><a className={active===link.key?styles.active:undefined} href={link.href} key={link.key}>{link.label}</a>)}</div>
    <a className={styles.workspaceLink} href="/workspace/settings">Settings</a>
    <details className={styles.mobile}><summary aria-label="Open product navigation"><span/><span/></summary><div>{links.map(link=><a href={link.href} key={link.key}>{link.label}</a>)}<a href="/workspace/settings">Settings</a></div></details>
  </nav></header>;
}

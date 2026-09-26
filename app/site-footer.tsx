import { RealveraxWordmark } from "./realverax-wordmark";
import styles from "./footer.module.css";

export function SiteFooter() {
  return (
    <footer className={styles.footer}>
      <div className={`${styles.inner} shell`}>
        <div className={styles.primary}>
          <div className={styles.brand}>
            <RealveraxWordmark size="footer" />
            <p>Independent verification for autonomous infrastructure changes.</p>
          </div>
          <nav className={styles.groups} aria-label="Footer navigation">
            <div className={styles.group}>
              <h2>Product</h2>
              <a href="/verifications">Verifications</a>
              <a href="/agents">Agents</a>
              <a href="/integrations">Integrations</a>
            </div>
            <div className={styles.group}>
              <h2>Explore</h2>
              <a href="/scenarios">Test cases</a>
              <a href="/environments">Environments</a>
              <a href="/pricing">Pricing</a>
            </div>
            <div className={styles.group}>
              <h2>Account</h2>
              <a href="/login">Sign in</a>
              <a href="/signup">Get started</a>
            </div>
          </nav>
        </div>
        <div className={styles.bottom}>
          <span>© {new Date().getFullYear()} realverax</span>
          <span>Verify what happens after the action.</span>
        </div>
      </div>
    </footer>
  );
}

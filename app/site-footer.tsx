import styles from "./footer.module.css";

export function SiteFooter() {
  const year = new Date().getFullYear();
  return (
    <footer className={styles.footer}>
      <div className={`${styles.inner} shell`}>
        <span>© {year} Realverax, Inc.</span>
        <nav className={styles.links} aria-label="Footer">
          <a href="/#verification-model">How it works</a>
          <a href="/scenarios">Test cases</a>
          <a href="/pricing">Pricing</a>
          <a href="/login">Sign in</a>
        </nav>
      </div>
    </footer>
  );
}

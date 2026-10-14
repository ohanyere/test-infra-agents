import { productName } from "./brand";
import { RealveraxWordmark } from "./realverax-wordmark";
import styles from "./footer.module.css";

const columns: { heading: string; links: { label: string; href: string }[] }[] = [
  {
    heading: "Product",
    links: [
      { label: "Verification model", href: "/#verification-model" },
      { label: "Integration status", href: "/integrations" },
      { label: "Create a workspace", href: "/signup" },
    ],
  },
  {
    heading: "Resources",
    links: [
      { label: "Why independent verification", href: "/#product" },
      { label: "Possible conclusions", href: "/#conclusions-title" },
      { label: "Pricing", href: "/pricing" },
    ],
  },
  {
    heading: "Company",
    links: [
      { label: "Pricing", href: "/pricing" },
      { label: "Sign in", href: "/login" },
      { label: "Get started", href: "/signup" },
    ],
  },
];

export function SiteFooter() {
  const year = new Date().getFullYear();
  return (
    <footer className={styles.footer}>
      <div className={`${styles.inner} shell`}>
        <div className={styles.brand}>
          <RealveraxWordmark size="footer" />
          <p>Independent verification for autonomous infrastructure changes.</p>
        </div>
        {columns.map((column) => (
          <nav key={column.heading} className={styles.column} aria-label={`Footer — ${column.heading}`}>
            <h2>{column.heading}</h2>
            <ul>
              {column.links.map((link) => (
                <li key={link.href + link.label}>
                  <a href={link.href}>{link.label}</a>
                </li>
              ))}
            </ul>
          </nav>
        ))}
      </div>
      <div className={`${styles.bottom} shell`}>
        <span>
          © {year} {productName}
        </span>
        <nav className={styles.bottomLinks} aria-label="Footer legal">
          <a href="/integrations">Integrations</a>
          <a href="/login">Sign in</a>
        </nav>
      </div>
    </footer>
  );
}

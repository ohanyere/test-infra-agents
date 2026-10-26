import { productName } from "./brand";
import styles from "./realverax-wordmark.module.css";

type WordmarkSize = "nav" | "footer";

export function RealveraxWordmark({
  href = "/",
  size = "nav",
  className = "",
}: {
  href?: string;
  size?: WordmarkSize;
  className?: string;
}) {
  return (
    <a
      aria-label={`${productName} home`}
      className={`${styles.wordmark} ${styles[size]} ${className}`.trim()}
      href={href}
    >
      {productName}
    </a>
  );
}

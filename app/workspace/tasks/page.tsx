import { ScenarioDirectory } from "../../scenarios/scenario-directory";
import styles from "../../product.module.css";

export default function WorkspaceTasksPage() {
  return <main className={styles.page}>
    <header className={styles.header}>
      <div>
        <p className={styles.eyebrow}>SCENARIOS</p>
        <h1>Infrastructure scenarios</h1>
        <p>Controlled conditions for independently verifying what happens after an agent acts.</p>
      </div>
      <div className={styles.actions}>
        <a href="/scenarios/new">Define scenario</a>
        <a className={styles.secondary} href="/scenarios">Public catalog</a>
      </div>
    </header>
    <ScenarioDirectory basePath="/workspace/tasks"/>
  </main>;
}

import { ScenarioDirectory } from "../../scenarios/scenario-directory";
import styles from "../../product.module.css";

export default function WorkspaceTasksPage() {
  return <main className={styles.page}>
    <header className={styles.header}>
      <div>
        <p className={styles.eyebrow}>TASKS</p>
        <h1>Evaluation tasks</h1>
        <p>Controlled operational problems used to establish infrastructure capability coverage.</p>
      </div>
      <div className={styles.actions}>
        <a href="/scenarios/new">Define task</a>
        <a className={styles.secondary} href="/scenarios">Public catalog</a>
      </div>
    </header>
    <ScenarioDirectory basePath="/workspace/tasks"/>
  </main>;
}

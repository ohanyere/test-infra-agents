import { ScenarioDirectory } from "../../scenarios/scenario-directory";
import styles from "../../product.module.css";

export default function WorkspaceTasksPage() {
  return <main className={styles.page}>
    <header className={styles.header}>
      <div>
        <p className={styles.eyebrow}>DEVELOPMENT TESTS</p>
        <h1>Infrastructure test cases</h1>
        <p>Supporting fixtures that create controlled autonomous changes whose resulting states can be independently verified.</p>
      </div>
      <div className={styles.actions}>
        <a href="/scenarios/new">Define test case</a>
        <a className={styles.secondary} href="/scenarios">Browse tests</a>
      </div>
    </header>
    <ScenarioDirectory basePath="/workspace/tasks"/>
  </main>;
}

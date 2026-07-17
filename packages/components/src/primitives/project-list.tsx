import styles from "./theme.module.css";

export type ProjectListEntry = {
  id: string;
  name: string;
  meta?: string;
};

export type ProjectListProps = {
  label: string;
  projects: ProjectListEntry[];
  onOpen(id: string): void;
};

export function ProjectList({ label, projects, onOpen }: ProjectListProps) {
  if (projects.length === 0) return null;
  return (
    <ul aria-label={label} className={styles.projectList}>
      {projects.map((project) => (
        <li key={project.id}>
          <button className={styles.projectListItem} type="button" onClick={() => onOpen(project.id)}>
            <span className={styles.projectListName}>{project.name}</span>
            {project.meta ? <span className={styles.projectListMeta}>{project.meta}</span> : null}
          </button>
        </li>
      ))}
    </ul>
  );
}

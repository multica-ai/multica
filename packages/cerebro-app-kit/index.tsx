import type { ButtonHTMLAttributes, InputHTMLAttributes, ReactNode, SelectHTMLAttributes } from "react";
import "./styles.css";

export function AppButton({ className = "", ...props }: ButtonHTMLAttributes<HTMLButtonElement>) {
  return <button className={`multica-app-button ${className}`.trim()} {...props} />;
}

type FieldProps = InputHTMLAttributes<HTMLInputElement> & { label: string; hint?: string };

export function AppField({ label, hint, id, name, className = "", ...props }: FieldProps) {
  const inputId = id ?? `app-field-${name}`;
  const hintId = hint ? `${inputId}-hint` : undefined;
  return <label className="multica-app-field" htmlFor={inputId}>
    <span>{label}</span>
    <input id={inputId} name={name} className={className} aria-describedby={hintId} {...props} />
    {hint && <small id={hintId}>{hint}</small>}
  </label>;
}

type SelectProps = SelectHTMLAttributes<HTMLSelectElement> & { label: string; children: ReactNode };

export function AppSelect({ label, id, name, children, ...props }: SelectProps) {
  const selectId = id ?? `app-select-${name}`;
  return <label className="multica-app-field" htmlFor={selectId}>
    <span>{label}</span>
    <select id={selectId} name={name} {...props}>{children}</select>
  </label>;
}

export function AppFormCard({ title, description, children }: { title: string; description?: string; children: ReactNode }) {
  return <section className="multica-app-card">
    <header><h2>{title}</h2>{description && <p>{description}</p>}</header>
    <div className="multica-app-card-content">{children}</div>
  </section>;
}

export type AppTableColumn<Row> = { key: keyof Row & string; label: string; render?: (value: Row[keyof Row], row: Row) => ReactNode };

export function AppTable<Row extends Record<string, unknown>>({ columns, rows, emptyLabel = "No results" }: { columns: AppTableColumn<Row>[]; rows: Row[]; emptyLabel?: string }) {
  return <div className="multica-app-table-wrap">
    <table className="multica-app-table">
      <thead><tr>{columns.map((column) => <th key={column.key} scope="col">{column.label}</th>)}</tr></thead>
      <tbody>{rows.length === 0
        ? <tr><td colSpan={columns.length}>{emptyLabel}</td></tr>
        : rows.map((row, index) => <tr key={index}>{columns.map((column) => <td key={column.key} data-label={column.label}>{column.render ? column.render(row[column.key], row) : String(row[column.key] ?? "")}</td>)}</tr>)}</tbody>
    </table>
  </div>;
}

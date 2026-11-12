import type { ReactNode } from 'react';
import type { Problem } from '@/lib/api';

export function PageHead({ kicker, title, sub, actions }: { kicker: string; title: string; sub?: ReactNode; actions?: ReactNode }) {
  return (
    <div className="page-head">
      <div>
        <div className="kicker">{kicker}</div>
        <h1>{title}</h1>
        {sub ? <div className="sub">{sub}</div> : null}
      </div>
      {actions ? <div className="head-actions">{actions}</div> : null}
    </div>
  );
}

export function Empty({ glyph, title, children }: { glyph?: string; title?: ReactNode; children?: ReactNode }) {
  return (
    <div className="empty">
      {glyph ? <div className="glyph">{glyph}</div> : null}
      {title ? <div className="title">{title}</div> : null}
      {children}
    </div>
  );
}

export function ErrorBox({ message, problems, children }: { message: string; problems?: Problem[]; children?: ReactNode }) {
  return (
    <div className="error-box" role="alert">
      {message}
      {problems && problems.length ? (
        <ul>
          {problems.map((p, i) => <li key={i}>{p.path ? <span className="path">{p.path}</span> : null}{p.message}</li>)}
        </ul>
      ) : null}
      {children}
    </div>
  );
}

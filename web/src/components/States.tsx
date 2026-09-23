import { AlertCircle, LoaderCircle } from "lucide-react";
import type { ReactNode } from "react";

export function LoadingState({ label = "Loading" }: { label?: string }) {
  return (
    <div className="grid min-h-48 place-items-center gap-2 p-8 text-sm text-zinc-400">
      <LoaderCircle className="animate-spin" size={22} />
      <span>{label}</span>
    </div>
  );
}
export function ErrorState({ message }: { message: string }) {
  return (
    <div
      role="alert"
      className="flex items-start gap-3 rounded-xl border border-red-900/60 bg-red-950/30 p-4 text-sm text-red-200"
    >
      <AlertCircle className="mt-0.5 shrink-0" size={18} />
      <span>{message}</span>
    </div>
  );
}
export function EmptyState({
  title,
  text,
  action,
}: {
  title: string;
  text: string;
  action?: ReactNode;
}) {
  return (
    <div className="grid place-items-center gap-2 rounded-2xl border border-dashed border-zinc-800 bg-zinc-900/30 p-12 text-center">
      <h2 className="text-lg font-semibold">{title}</h2>
      <p className="max-w-md text-sm text-zinc-400">{text}</p>
      {action}
    </div>
  );
}
export function PageHeader({
  eyebrow,
  title,
  children,
}: {
  eyebrow?: string;
  title: ReactNode;
  children?: ReactNode;
}) {
  return (
    <div className="mb-8 flex flex-wrap items-end justify-between gap-4">
      <div>
        {eyebrow && (
          <p className="mb-2 text-xs font-semibold uppercase tracking-[0.18em] text-amber-400">
            {eyebrow}
          </p>
        )}
        <h1 className="text-3xl font-semibold tracking-tight sm:text-4xl">{title}</h1>
      </div>
      {children}
    </div>
  );
}

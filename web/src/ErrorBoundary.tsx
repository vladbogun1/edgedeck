import { AlertTriangle, RotateCw } from "lucide-react";
import { Component, type ReactNode } from "react";

// Предохранитель: ошибка в одном разделе показывает сообщение, а не роняет
// всё приложение в чёрный экран.
export class ErrorBoundary extends Component<{ children: ReactNode; resetKey?: string }, { error: Error | null }> {
  state = { error: null as Error | null };

  static getDerivedStateFromError(error: Error) {
    return { error };
  }

  componentDidUpdate(prev: { resetKey?: string }) {
    if (prev.resetKey !== this.props.resetKey && this.state.error) this.setState({ error: null });
  }

  render() {
    if (!this.state.error) return this.props.children;
    return (
      <div className="callout flex gap-3 [--tone:var(--color-bad)]">
        <AlertTriangle className="mt-0.5 size-4 shrink-0 text-bad" />
        <div className="min-w-0 flex-1">
          <div className="font-medium text-bad">Раздел не отрисовался</div>
          <div className="mt-1 font-mono text-xs break-all text-muted">{this.state.error.message}</div>
          <button onClick={() => this.setState({ error: null })} className="btn btn-ghost btn-sm mt-3">
            <RotateCw className="size-3.5" />
            Попробовать ещё раз
          </button>
        </div>
      </div>
    );
  }
}

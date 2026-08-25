import { Component, type ReactNode } from "react";

type ErrorBoundaryProps = { children: ReactNode };
type ErrorBoundaryState = { error: Error | undefined };

// A render-phase exception below the router would otherwise unmount the whole
// document and leave a blank page. The boundary keeps the surrounding shell
// usable and shows the failure instead.
export class ErrorBoundary extends Component<ErrorBoundaryProps, ErrorBoundaryState> {
  state: ErrorBoundaryState = { error: undefined };

  static getDerivedStateFromError(error: Error): ErrorBoundaryState {
    return { error };
  }

  componentDidUpdate(previous: ErrorBoundaryProps) {
    // Navigating away swaps the children, which ends the crashed subtree;
    // clear the state so the next page renders normally.
    if (previous.children !== this.props.children && this.state.error) {
      this.setState({ error: undefined });
    }
  }

  render() {
    if (this.state.error) {
      return (
        <div className="mx-auto max-w-2xl space-y-3 p-8">
          <h1 className="text-lg font-semibold">Something went wrong</h1>
          <p className="text-sm text-zinc-400">
            The page crashed while rendering. Navigating away or retrying resets it; the error below
            helps report the cause.
          </p>
          <pre className="overflow-auto rounded-lg border border-red-900/60 bg-red-950/30 p-3 text-xs whitespace-pre-wrap text-red-200">
            {this.state.error.message}
          </pre>
          <button
            onClick={() => this.setState({ error: undefined })}
            className="rounded-lg border border-zinc-700 px-3 py-2 text-sm"
          >
            Try again
          </button>
        </div>
      );
    }
    return this.props.children;
  }
}

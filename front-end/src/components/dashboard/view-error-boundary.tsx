"use client";

import * as React from "react";
import { AlertTriangle, RefreshCw } from "lucide-react";
import { Button } from "@/components/ui/button";

interface Props {
  children: React.ReactNode;
  viewName?: string;
}

interface State {
  hasError: boolean;
  error?: Error;
}

/**
 * Error Boundary qui empêche l'écran blanc "Application error".
 * Affiche un message convivial avec un bouton pour réessayer.
 */
export class ViewErrorBoundary extends React.Component<Props, State> {
  constructor(props: Props) {
    super(props);
    this.state = { hasError: false };
  }

  static getDerivedStateFromError(error: Error): State {
    return { hasError: true, error };
  }

  componentDidCatch(error: Error, errorInfo: React.ErrorInfo) {
    console.error(`[ViewErrorBoundary${this.props.viewName ? ` — ${this.props.viewName}` : ""}]`, error, errorInfo);
  }

  handleRetry = () => {
    this.setState({ hasError: false, error: undefined });
  };

  render() {
    if (this.state.hasError) {
      return (
        <div className="flex flex-col items-center justify-center gap-4 py-16 text-center">
          <AlertTriangle className="size-12 text-amber-accent" />
          <div>
            <h3 className="text-lg font-semibold">
              Une erreur est survenue
            </h3>
            <p className="mt-1 max-w-md text-sm text-muted-foreground">
              {this.props.viewName
                ? `La vue "${this.props.viewName}" n'a pas pu se charger.`
                : "Cette section n'a pas pu se charger."}
              {" "}Vous pouvez réessayer ou revenir plus tard.
            </p>
            {this.state.error?.message && (
              <p className="mt-2 text-xs text-muted-foreground/70">
                Détail : {this.state.error.message}
              </p>
            )}
          </div>
          <Button onClick={this.handleRetry} variant="outline" className="gap-2">
            <RefreshCw className="size-4" /> Réessayer
          </Button>
        </div>
      );
    }
    return this.props.children;
  }
}

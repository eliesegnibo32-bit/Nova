"use client";

import * as React from "react";
import { UserPlus, MoreHorizontal, Mail, Shield } from "lucide-react";
import { toast } from "sonner";

import { Button } from "@/components/ui/button";
import { Badge } from "@/components/ui/badge";
import { Card } from "@/components/ui/card";
import { Avatar, AvatarFallback } from "@/components/ui/avatar";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
  DialogDescription,
  DialogFooter,
} from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import {
  teamMembers as allMembers,
  teamRoleLabel,
  type TeamRole,
} from "@/lib/mock-data";
import { initials } from "@/lib/format";
import { ViewHeader } from "./view-header";

function roleBadge(role: TeamRole) {
  if (role === "owner")
    return (
      <Badge className="bg-brand/10 text-brand hover:bg-brand/20">
        <Shield className="size-3" /> {teamRoleLabel.owner}
      </Badge>
    );
  if (role === "manager")
    return <Badge variant="secondary">{teamRoleLabel.manager}</Badge>;
  return <Badge variant="outline">{teamRoleLabel.employee}</Badge>;
}

export function TeamView() {
  const [inviteOpen, setInviteOpen] = React.useState(false);
  const [inviteName, setInviteName] = React.useState("");
  const [inviteEmail, setInviteEmail] = React.useState("");
  const [inviteRole, setInviteRole] = React.useState<string>("employee");

  function submitInvite(e: React.FormEvent) {
    e.preventDefault();
    if (!inviteName || !inviteEmail) return;
    toast.success(`Invitation envoyée à ${inviteName}`, {
      description: `${inviteEmail} — ${teamRoleLabel[inviteRole as TeamRole]}`,
    });
    setInviteOpen(false);
    setInviteName("");
    setInviteEmail("");
    setInviteRole("employee");
  }

  return (
    <div className="space-y-5">
      <ViewHeader
        title="Équipe"
        description="Invitez vos employés et gérez leurs permissions par boutique."
        actions={
          <Button size="sm" onClick={() => setInviteOpen(true)} className="gap-1.5">
            <UserPlus className="size-4" /> Inviter un employé
          </Button>
        }
      />

      <Card className="overflow-hidden">
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead>Nom</TableHead>
              <TableHead className="hidden md:table-cell">Email</TableHead>
              <TableHead>Rôle</TableHead>
              <TableHead className="hidden lg:table-cell">Permissions</TableHead>
              <TableHead>Statut</TableHead>
              <TableHead className="w-[60px]"></TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {allMembers.map((m) => (
              <TableRow key={m.id}>
                <TableCell>
                  <div className="flex items-center gap-3">
                    <Avatar className="size-9">
                      <AvatarFallback className="bg-brand/10 text-xs font-semibold text-brand">
                        {initials(m.name)}
                      </AvatarFallback>
                    </Avatar>
                    <div>
                      <div className="font-medium">{m.name}</div>
                      <div className="text-xs text-muted-foreground md:hidden">
                        {m.email}
                      </div>
                    </div>
                  </div>
                </TableCell>
                <TableCell className="hidden text-muted-foreground md:table-cell">
                  <span className="inline-flex items-center gap-1.5">
                    <Mail className="size-3.5" />
                    {m.email}
                  </span>
                </TableCell>
                <TableCell>{roleBadge(m.role)}</TableCell>
                <TableCell className="hidden lg:table-cell">
                  <div className="flex flex-wrap gap-1">
                    {m.permissions.slice(0, 3).map((p) => (
                      <Badge key={p} variant="outline" className="text-[10px]">
                        {p}
                      </Badge>
                    ))}
                    {m.permissions.length > 3 && (
                      <Badge variant="outline" className="text-[10px]">
                        +{m.permissions.length - 3}
                      </Badge>
                    )}
                  </div>
                </TableCell>
                <TableCell>
                  {m.status === "active" ? (
                    <Badge className="bg-emerald-500/10 text-emerald-700 dark:text-emerald-400 hover:bg-emerald-500/20">
                      Actif
                    </Badge>
                  ) : (
                    <Badge className="bg-amber-accent/15 text-amber-accent hover:bg-amber-accent/25">
                      Invité
                    </Badge>
                  )}
                </TableCell>
                <TableCell>
                  <DropdownMenu>
                    <DropdownMenuTrigger asChild>
                      <Button
                        variant="ghost"
                        size="icon"
                        className="size-8"
                        aria-label="Actions membre"
                      >
                        <MoreHorizontal className="size-4" />
                      </Button>
                    </DropdownMenuTrigger>
                    <DropdownMenuContent align="end">
                      <DropdownMenuItem
                        onClick={() => toast.info(`Modifier ${m.name}`)}
                      >
                        Modifier le rôle
                      </DropdownMenuItem>
                      <DropdownMenuItem
                        onClick={() => toast.info(`Renvoyer l'invitation à ${m.email}`)}
                      >
                        Renvoyer l'invitation
                      </DropdownMenuItem>
                      <DropdownMenuItem
                        className="text-destructive focus:text-destructive"
                        onClick={() => toast.error(`Retirer ${m.name} ?`)}
                      >
                        Retirer de l'équipe
                      </DropdownMenuItem>
                    </DropdownMenuContent>
                  </DropdownMenu>
                </TableCell>
              </TableRow>
            ))}
          </TableBody>
        </Table>
      </Card>

      <div className="rounded-lg border border-dashed bg-muted/30 p-3 text-xs text-muted-foreground">
        Votre plan <strong className="text-foreground">{`Essentiel`}</strong>{" "}
        inclut jusqu'à <strong className="text-foreground">3 employés</strong>.
        Passez au plan supérieur pour inviter plus de membres.
      </div>

      <Dialog open={inviteOpen} onOpenChange={setInviteOpen}>
        <DialogContent className="sm:max-w-md">
          <DialogHeader>
            <DialogTitle>Inviter un employé</DialogTitle>
            <DialogDescription>
              L'employé recevra un email d'activation pour rejoindre votre
              boutique.
            </DialogDescription>
          </DialogHeader>
          <form onSubmit={submitInvite} className="space-y-3">
            <div className="space-y-1.5">
              <Label htmlFor="invite-name">Nom complet</Label>
              <Input
                id="invite-name"
                placeholder="Ex : Aya Bamba"
                value={inviteName}
                onChange={(e) => setInviteName(e.target.value)}
                required
              />
            </div>
            <div className="space-y-1.5">
              <Label htmlFor="invite-email">Adresse e-mail</Label>
              <Input
                id="invite-email"
                type="email"
                placeholder="aya@boutique.ci"
                value={inviteEmail}
                onChange={(e) => setInviteEmail(e.target.value)}
                required
              />
            </div>
            <div className="space-y-1.5">
              <Label htmlFor="invite-role">Rôle</Label>
              <Select value={inviteRole} onValueChange={setInviteRole}>
                <SelectTrigger id="invite-role" className="w-full">
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value="employee">Employé</SelectItem>
                  <SelectItem value="manager">Gérant</SelectItem>
                </SelectContent>
              </Select>
            </div>
            <DialogFooter>
              <Button
                type="button"
                variant="outline"
                onClick={() => setInviteOpen(false)}
              >
                Annuler
              </Button>
              <Button type="submit">
                <UserPlus className="size-4" /> Envoyer l'invitation
              </Button>
            </DialogFooter>
          </form>
        </DialogContent>
      </Dialog>
    </div>
  );
}

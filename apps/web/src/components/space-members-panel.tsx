import { useState } from "react";
import { MoreHorizontal, Trash2 } from "lucide-react";
import { Button } from "@workspace/ui/components/button";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@workspace/ui/components/dialog";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from "@workspace/ui/components/dropdown-menu";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@workspace/ui/components/select";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@workspace/ui/components/table";
import { Avatar, AvatarFallback, AvatarImage } from "@workspace/ui/components/avatar";
import { Badge } from "@workspace/ui/components/badge";
import {
  useSpaceMembers,
  useAddSpaceMember,
  useUpdateMemberRole,
  useRemoveSpaceMember,
} from "@/lib/queries";
import type { SpaceMember, UserSearchResult } from "@/lib/api/types";
import { MemberPicker } from "./member-picker";

interface SpaceMembersPanelProps {
  spaceId: string;
  currentUserRole: string;
}

export function SpaceMembersPanel({ spaceId, currentUserRole }: SpaceMembersPanelProps) {
  const { data: members = [], isLoading } = useSpaceMembers(spaceId);
  const addMember = useAddSpaceMember();
  const updateRole = useUpdateMemberRole();
  const removeMember = useRemoveSpaceMember();

  const [addOpen, setAddOpen] = useState(false);
  const [selectedUser, setSelectedUser] = useState<UserSearchResult | null>(null);
  const [selectedRole, setSelectedRole] = useState<SpaceMember["role"]>("viewer");

  const isOwner = currentUserRole === "owner";
  const memberIds = members.map((m) => m.user_id);

  const handleAdd = () => {
    if (!selectedUser) return;
    addMember.mutate(
      { spaceId, user_id: selectedUser.id, role: selectedRole },
      {
        onSuccess: () => {
          setAddOpen(false);
          setSelectedUser(null);
          setSelectedRole("viewer");
        },
      },
    );
  };

  const handleRoleChange = (userId: string, role: SpaceMember["role"]) => {
    updateRole.mutate({ spaceId, userId, role });
  };

  const handleRemove = (userId: string) => {
    removeMember.mutate({ spaceId, userId });
  };

  if (isLoading) {
    return <p className="text-muted-foreground p-4">Loading members...</p>;
  }

  return (
    <div className="space-y-4">
      <div className="flex items-center justify-between">
        <h3 className="text-lg font-medium">Members</h3>
        {isOwner && (
          <Button size="sm" onClick={() => setAddOpen(true)}>
            Add Member
          </Button>
        )}
      </div>

      <Table>
        <TableHeader>
          <TableRow>
            <TableHead>User</TableHead>
            <TableHead>Role</TableHead>
            {isOwner && <TableHead className="w-[50px]" />}
          </TableRow>
        </TableHeader>
        <TableBody>
          {members.map((member) => (
            <TableRow key={member.user_id}>
              <TableCell>
                <div className="flex items-center gap-2">
                  <Avatar className="h-8 w-8">
                    <AvatarImage src={member.avatar_url} />
                    <AvatarFallback>
                      {(member.display_name || member.name || member.email)
                        .slice(0, 2)
                        .toUpperCase()}
                    </AvatarFallback>
                  </Avatar>
                  <div>
                    <p className="font-medium">{member.display_name || member.name}</p>
                    <p className="text-muted-foreground text-xs">{member.email}</p>
                  </div>
                </div>
              </TableCell>
              <TableCell>
                {isOwner ? (
                  <Select
                    value={member.role}
                    onValueChange={(v) =>
                      handleRoleChange(member.user_id, v as SpaceMember["role"])
                    }
                  >
                    <SelectTrigger className="w-[100px]">
                      <SelectValue />
                    </SelectTrigger>
                    <SelectContent>
                      <SelectItem value="viewer">Viewer</SelectItem>
                      <SelectItem value="editor">Editor</SelectItem>
                      <SelectItem value="owner">Owner</SelectItem>
                    </SelectContent>
                  </Select>
                ) : (
                  <Badge variant={member.role === "owner" ? "default" : "secondary"}>
                    {member.role}
                  </Badge>
                )}
              </TableCell>
              {isOwner && (
                <TableCell>
                  <DropdownMenu>
                    <DropdownMenuTrigger
                      render={<Button variant="ghost" size="icon" className="h-8 w-8" />}
                    >
                      <MoreHorizontal className="h-4 w-4" />
                    </DropdownMenuTrigger>
                    <DropdownMenuContent align="end">
                      <DropdownMenuItem
                        className="text-destructive"
                        onClick={() => handleRemove(member.user_id)}
                      >
                        <Trash2 className="mr-2 h-4 w-4" />
                        Remove
                      </DropdownMenuItem>
                    </DropdownMenuContent>
                  </DropdownMenu>
                </TableCell>
              )}
            </TableRow>
          ))}
        </TableBody>
      </Table>

      <Dialog open={addOpen} onOpenChange={setAddOpen}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>Add Member</DialogTitle>
            <DialogDescription>Search for a user to add to this space.</DialogDescription>
          </DialogHeader>
          <div className="space-y-4 py-4">
            <MemberPicker
              onSelect={setSelectedUser}
              excludeIds={memberIds}
              placeholder={
                selectedUser ? selectedUser.display_name || selectedUser.name : "Select user..."
              }
            />
            {selectedUser && (
              <Select
                value={selectedRole}
                onValueChange={(v) => setSelectedRole(v as SpaceMember["role"])}
              >
                <SelectTrigger>
                  <SelectValue placeholder="Select role" />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value="viewer">Viewer</SelectItem>
                  <SelectItem value="editor">Editor</SelectItem>
                  <SelectItem value="owner">Owner</SelectItem>
                </SelectContent>
              </Select>
            )}
          </div>
          <DialogFooter>
            <Button variant="outline" onClick={() => setAddOpen(false)}>
              Cancel
            </Button>
            <Button onClick={handleAdd} disabled={!selectedUser || addMember.isPending}>
              {addMember.isPending ? "Adding..." : "Add"}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </div>
  );
}

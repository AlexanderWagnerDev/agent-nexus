# WSL hosts

WSL distributions are **separate hosts**. Agents installed inside WSL keep
their session stores inside the distribution's filesystem. The Windows-side
`nexusd` does not read WSL stores.

## Setup

1. Install and log in to the agents inside the distribution as the normal WSL
   user.
2. Install `nexusd` inside the distribution (Linux build).
3. Pair it with the hub from inside WSL:

   ```bash
   nexusd pair --hub https://nexus.example.com --code XXXX-XXXX --name wsl-ubuntu
   nexusd service install
   nexusd doctor
   ```

4. The host policy's `allowed_roots` must contain the WSL project paths, for
   example `/home/demo/projects` or `/mnt/c/Users/demo/source`.

## Notes

- Do not use the Windows `nexusd` binary from inside WSL.
- `/mnt/c` access is slower and may have different permission semantics; native
  WSL paths are recommended.
- The systemd user unit may require `loginctl enable-linger "$USER"` if the
  distribution is not kept running after logout.
- Use a distinct host name for each distribution; AgentNexus treats them as
  independent machines.
- This path is documented but was not exercised on a WSL installation in the
  0.1.x development environment.

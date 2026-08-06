<h3>💰 Infracost report</h3>

<p>Consider fixing these issues, they don't align with your company's FinOps policies & the Well-Architected Framework. <b>Add a PR comment with <code>@infracost help</code> to see how you can dismiss or snooze issues and unblock your PR.</b></p>

<table>
  <tr><td colspan="2" width="1000px">FinOps policies</td></tr>
  
<tr><td colspan="2" title="Failure">
<details >
<summary>
<b>🔴 Use reserved instances</b>
</summary><br/>

Consider using reserved instances for long-running workloads.
</details>
</td></tr>
<tr><td></td><td>

resource [`aws_instance.web`](https://github.com/my-org/my-repo/blob/def456/main.tf#L15)
  * This instance runs 24/7 and could benefit from a reserved instance
    * 🔧 [Fix in your IDE](https://cost.dev/?utm_source=pr_comment&utm_content=fix_in_ide) — or ask your agent to apply it with Infracost Dev

in projects `prod`, `staging`

  * This instance is oversized for its workload
    * 🔧 [Fix in your IDE](https://cost.dev/?utm_source=pr_comment&utm_content=fix_in_ide) — or ask your agent to apply it with Infracost Dev

in project `dev`
</td></tr>

  </table>

<hr/>

![Infracost Dev](https://img.shields.io/badge/Infracost-Dev-db2777?labelColor=000)

**Let your coding agent remediate these.** Infracost Dev gives Cursor, Claude Code and Copilot your FinOps policies and live cloud pricing — so the next PR ships clean.

[cost.dev](https://cost.dev/?utm_source=pr_comment&utm_content=infracost_dev_promo) · [Setup guide](https://www.infracost.io/docs/?utm_source=pr_comment&utm_content=infracost_dev_promo)

<sub>
  This comment will be updated when code changes.
</sub>


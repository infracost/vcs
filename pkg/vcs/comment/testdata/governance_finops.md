<h3>💰 Infracost report</h3>

<p>Consider fixing this issue, it doesn't align with your company's FinOps policies & the Well-Architected Framework. <b>Add a PR comment with <code>@infracost help</code> to see how you can dismiss or snooze issues and unblock your PR.</b></p>

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

resource [aws_instance.web](https://github.com/my-org/my-repo/blob/def456/main.tf#L15)
  * This instance runs 24/7 and could benefit from a reserved instance
    in project `my-project`
    </td></tr>

  </table>

<sub>
  This comment will be updated when code changes.
</sub>


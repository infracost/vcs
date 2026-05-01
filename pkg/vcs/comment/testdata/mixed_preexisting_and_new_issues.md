<h3>💰 Infracost report</h3>

<p>Consider fixing this issue, it doesn't align with your company's FinOps policies & the Well-Architected Framework. <b>Add a PR comment with <code>@infracost help</code> to see how you can dismiss or snooze issues and unblock your PR.</b></p>

<table>
  <tr><td colspan="2" width="1000px">FinOps policies</td></tr>
  
<tr><td colspan="2" title="Failure">
  <details >
    <summary>
    <b>🔴 Use GP3 volumes</b>
  </summary><br/>

Use GP3 volumes instead of GP2 for better performance.
  </details>
</td></tr>
<tr><td></td><td>

`aws_ebs_volume.new`
  * This volume uses GP2, consider upgrading to GP3
    </td></tr>

  </table>

<sub>
  This comment will be updated when code changes.
</sub>


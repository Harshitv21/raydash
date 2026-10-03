package raydash.demo.app.user.models;

import jakarta.persistence.Column;
import jakarta.persistence.Entity;
import jakarta.persistence.Id;
import jakarta.persistence.Table;
import lombok.AllArgsConstructor;
import lombok.Builder;
import lombok.Getter;
import lombok.NoArgsConstructor;
import lombok.Setter;

@Entity 
@Builder
@Getter 
@Setter
@NoArgsConstructor 
@AllArgsConstructor
@Table(name = "users")
public class UserEntity {
    @Id
    @Column(name = "user_id")
    private Long userId;

    @Column(name = "name")
    private String name;
    
    @Column(name = "email")
    private String email;
}
